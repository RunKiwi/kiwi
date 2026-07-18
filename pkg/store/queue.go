package store

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// newLeaseID mints a random fencing token for a lease.
func newLeaseID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "lease_" + hex.EncodeToString(b), nil
}

// EnqueueTask adds a task to the queue in QUEUED state.
func (s *PostgresStore) EnqueueTask(ctx context.Context, task *QueuedTask) error {
	if task.Status == "" {
		task.Status = TaskQueued
	}
	return s.db.WithContext(ctx).Create(task).Error
}

// leaseCandidateBatch bounds how many oldest QUEUED rows LeaseNextTask inspects
// per call when looking for a dependency-satisfied task. A task blocked on an
// unfinished dependency must not stall the whole queue, so we scan past it to
// the first leasable task — but only within this window, to keep the per-call
// row-lock footprint bounded. At current scale (few in-flight jobs per org)
// this comfortably covers a job's DAG; a normalized dependency table would let
// the predicate move into a pure SQL WHERE and drop the window entirely.
const leaseCandidateBatch = 64

// LeaseNextTask atomically claims the oldest leasable QUEUED task for orgID,
// marking it LEASED to leasedBy with a fresh fencing LeaseID for ttl. It returns
// (nil, nil) when no leasable task is available.
//
// A task is leasable only when every task in its plan `depends_on` has already
// SUCCEEDED (DAG enforcement — see the Execution Model RFC §3). This is what
// keeps a `verify` worker from running before the `impl` workers it depends on.
// Dependencies are worker IDs stored in the spec; the corresponding sibling task
// id is `<job_id>-<worker_id>`. Because SUCCEEDED is terminal, a dependency read
// as satisfied cannot later revert, so the check needs no extra locking.
//
// On Postgres the candidate rows are selected FOR UPDATE SKIP LOCKED so
// concurrent daemons never claim the same task; on other dialects (e.g. the
// SQLite used in tests) the conditional UPDATE (WHERE status = QUEUED) provides
// the same no-double-lease guarantee.
func (s *PostgresStore) LeaseNextTask(ctx context.Context, orgID, leasedBy string, ttl time.Duration) (*QueuedTask, error) {
	leaseID, err := newLeaseID()
	if err != nil {
		return nil, err
	}

	var leased *QueuedTask
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		q := tx.Where("org_id = ? AND status = ?", orgID, TaskQueued).
			Order("created_at ASC, id ASC").
			Limit(leaseCandidateBatch)
		if tx.Dialector.Name() == "postgres" {
			q = q.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"})
		}

		var candidates []QueuedTask
		if err := q.Find(&candidates).Error; err != nil {
			return err
		}
		if len(candidates) == 0 {
			return nil // no work available
		}

		candidate, err := firstLeasable(tx, orgID, candidates)
		if err != nil {
			return err
		}
		if candidate == nil {
			return nil // work exists but all of it is blocked on unfinished deps
		}

		now := time.Now()
		expires := now.Add(ttl)
		res := tx.Model(&QueuedTask{}).
			Where("id = ? AND status = ?", candidate.ID, TaskQueued).
			Updates(map[string]interface{}{
				"status":           TaskLeased,
				"leased_by":        leasedBy,
				"lease_id":         leaseID,
				"lease_expires_at": expires,
				"attempts":         gorm.Expr("attempts + 1"),
				"updated_at":       now,
			})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return nil // lost the race to another leaser; caller may retry
		}

		var fresh QueuedTask
		if err := tx.First(&fresh, "id = ?", candidate.ID).Error; err != nil {
			return err
		}
		leased = &fresh
		return nil
	})
	return leased, err
}

// firstLeasable returns the first candidate (in the given order) whose plan
// dependencies have all SUCCEEDED, or nil if every candidate is still blocked.
// It resolves each candidate's dependency worker IDs to sibling task ids and
// looks up their statuses in a single query, so the scan is O(1) round-trips
// regardless of batch size.
func firstLeasable(tx *gorm.DB, orgID string, candidates []QueuedTask) (*QueuedTask, error) {
	// Collect the union of dependency task ids across all candidates.
	depSet := make(map[string]struct{})
	for i := range candidates {
		for _, id := range dependencyTaskIDs(&candidates[i]) {
			depSet[id] = struct{}{}
		}
	}

	statuses := make(map[string]string, len(depSet))
	if len(depSet) > 0 {
		ids := make([]string, 0, len(depSet))
		for id := range depSet {
			ids = append(ids, id)
		}
		var deps []QueuedTask
		if err := tx.Select("id", "status").
			Where("org_id = ? AND id IN ?", orgID, ids).
			Find(&deps).Error; err != nil {
			return nil, err
		}
		for _, d := range deps {
			statuses[d.ID] = d.Status
		}
	}

	for i := range candidates {
		if dependenciesSatisfied(&candidates[i], statuses) {
			return &candidates[i], nil
		}
	}
	return nil, nil
}

// dependenciesSatisfied reports whether every dependency of t has SUCCEEDED. A
// dependency task id absent from statuses (never enqueued, or already reaped)
// is treated as unsatisfied — the safe default is to hold the task back rather
// than run a worker whose predecessor cannot be confirmed done.
func dependenciesSatisfied(t *QueuedTask, statuses map[string]string) bool {
	for _, id := range dependencyTaskIDs(t) {
		if statuses[id] != TaskSucceeded {
			return false
		}
	}
	return true
}

// dependencyTaskIDs resolves a task's `depends_on` worker IDs (as stored in its
// spec by the planner) to the sibling task ids that carry them: `<job_id>-<id>`.
// A task with no dependencies returns nil and is always leasable.
func dependencyTaskIDs(t *QueuedTask) []string {
	raw, ok := t.Spec["depends_on"]
	if !ok || raw == nil {
		return nil
	}
	arr, ok := raw.([]interface{})
	if !ok {
		return nil
	}
	ids := make([]string, 0, len(arr))
	for _, v := range arr {
		if dep, ok := v.(string); ok && dep != "" {
			ids = append(ids, t.JobID+"-"+dep)
		}
	}
	return ids
}

// RenewLease extends the lease on taskID iff the presented leaseID still owns
// it and the task is still LEASED. Returns false if the token is stale.
func (s *PostgresStore) RenewLease(ctx context.Context, taskID, leaseID string, ttl time.Duration) (bool, error) {
	now := time.Now()
	res := s.db.WithContext(ctx).Model(&QueuedTask{}).
		Where("id = ? AND lease_id = ? AND status = ?", taskID, leaseID, TaskLeased).
		Updates(map[string]interface{}{
			"lease_expires_at": now.Add(ttl),
			"updated_at":       now,
		})
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
}

// CompleteTask marks taskID terminal (SUCCEEDED or FAILED) iff the presented
// leaseID still owns it. The fencing check prevents a stale daemon from
// completing a task that has since been requeued and reassigned.
func (s *PostgresStore) CompleteTask(ctx context.Context, taskID, leaseID, finalStatus string) (bool, error) {
	if finalStatus != TaskSucceeded && finalStatus != TaskFailed {
		return false, fmt.Errorf("invalid final status %q (want %s or %s)", finalStatus, TaskSucceeded, TaskFailed)
	}
	now := time.Now()
	res := s.db.WithContext(ctx).Model(&QueuedTask{}).
		Where("id = ? AND lease_id = ? AND status = ?", taskID, leaseID, TaskLeased).
		Updates(map[string]interface{}{
			"status":     finalStatus,
			"updated_at": now,
		})
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
}

// RequeueExpiredLeases returns any LEASED task whose lease has lapsed back to
// QUEUED (clearing its lease fields so another daemon can claim it) and returns
// the number of tasks recovered. Tasks already leased MaxLeaseAttempts times are
// instead dead-lettered to FAILED — a poison-pill guard so a spec that reliably
// crashes its daemon isn't requeued forever. Run this periodically as a sweeper.
func (s *PostgresStore) RequeueExpiredLeases(ctx context.Context) (int, error) {
	now := time.Now()

	var requeued int
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Dead-letter poison tasks first so the requeue below skips them.
		if err := tx.Model(&QueuedTask{}).
			Where("status = ? AND lease_expires_at < ? AND attempts >= ?", TaskLeased, now, MaxLeaseAttempts).
			Updates(map[string]interface{}{
				"status":     TaskFailed,
				"updated_at": now,
			}).Error; err != nil {
			return err
		}

		res := tx.Model(&QueuedTask{}).
			Where("status = ? AND lease_expires_at < ? AND attempts < ?", TaskLeased, now, MaxLeaseAttempts).
			Updates(map[string]interface{}{
				"status":           TaskQueued,
				"leased_by":        nil,
				"lease_id":         nil,
				"lease_expires_at": nil,
				"updated_at":       now,
			})
		if res.Error != nil {
			return res.Error
		}
		requeued = int(res.RowsAffected)
		return nil
	})
	if err != nil {
		return 0, err
	}
	return requeued, nil
}
