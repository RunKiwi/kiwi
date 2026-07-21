package store

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
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

// shortRepo reduces a git remote URL to "owner/name" for compact display,
// e.g. "https://github.com/acme/api.git" -> "acme/api". Falls back to the
// trimmed input when it can't be parsed.
func shortRepo(url string) string {
	s := strings.TrimSpace(url)
	if s == "" {
		return ""
	}
	s = strings.TrimSuffix(s, ".git")
	// Drop scheme / host: keep the last two path segments.
	if i := strings.Index(s, "://"); i >= 0 {
		s = s[i+3:]
	}
	s = strings.TrimPrefix(s, "git@")
	s = strings.ReplaceAll(s, ":", "/")
	parts := strings.Split(strings.Trim(s, "/"), "/")
	if len(parts) >= 2 {
		return parts[len(parts)-2] + "/" + parts[len(parts)-1]
	}
	return strings.Trim(s, "/")
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
// fleetID scopes routing: the caller (a daemon) leases only tasks assigned to
// its own fleet, plus tasks with no fleet assignment (fleet_id = ”), which run
// on any daemon in the org. A task pinned to a fleet therefore never runs on a
// different fleet's daemon; an unassigned task is never stranded as long as any
// daemon exists. A daemon with no fleet (fleetID = "") leases only unassigned
// tasks.
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
func (s *PostgresStore) LeaseNextTask(ctx context.Context, orgID, leasedBy, fleetID string, ttl time.Duration) (*QueuedTask, error) {
	leaseID, err := newLeaseID()
	if err != nil {
		return nil, err
	}

	var leased *QueuedTask
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Enforce OrgLimits for concurrency cap
		var limits OrgLimits
		if err := tx.First(&limits, "org_id = ?", orgID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				limits = OrgLimits{
					OrgID:              orgID,
					MaxConcurrentJobs:  10,
					MaxBudgetPerJob:    5.00,
					MaxBudgetPerMonth:  500.00,
					MaxWorkersPerJob:   8,
					TaskTimeoutSeconds: 1800,
					MaxSandboxDiskMB:   2048,
				}
			} else {
				return err
			}
		}

		var inFlight int64
		if err := tx.Model(&QueuedTask{}).Where("org_id = ? AND status = ?", orgID, TaskLeased).Count(&inFlight).Error; err != nil {
			return err
		}
		if inFlight >= int64(limits.MaxConcurrentJobs) {
			return nil // Org at cap, refuse new lease
		}

		if limits.MaxAgentMinutesPerMonth > 0 {
			var usedMinutes float64
			now := time.Now().UTC()
			monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
			if err := tx.Model(&Job{}).Where("org_id = ? AND created_at >= ?", orgID, monthStart).Select("COALESCE(SUM(agent_minutes), 0)").Scan(&usedMinutes).Error; err != nil {
				return err
			}
			if usedMinutes >= limits.MaxAgentMinutesPerMonth {
				return nil // Org at compute cap, refuse new lease
			}
		}

		// Fleet routing: a daemon leases work for its own fleet, plus unassigned
		// work (fleet_id = '') that may run anywhere.
		q := tx.Where("org_id = ? AND status = ? AND (fleet_id = ? OR fleet_id = ?)", orgID, TaskQueued, fleetID, "").
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

		for len(candidates) > 0 {
			candidate, err := firstLeasable(tx, orgID, candidates)
			if err != nil {
				return err
			}
			if candidate == nil {
				return nil // work exists but all of it is blocked on unfinished deps
			}

			// Enforce per-job budget cap
			if candidate.JobID != "" {
				var job Job
				if err := tx.Select("cost_usd").First(&job, "id = ?", candidate.JobID).Error; err == nil {
					if job.CostUSD >= limits.MaxBudgetPerJob {
						reason := "Job exceeded maximum budget per job cap"
						tx.Model(&QueuedTask{}).Where("id = ?", candidate.ID).Updates(map[string]interface{}{
							"status":        TaskFailed,
							"result_detail": &reason,
							"updated_at":    time.Now(),
						})
						tx.Model(&Job{}).Where("id = ?", candidate.JobID).Updates(map[string]interface{}{
							"status":     "FAILED",
							"error":      &reason,
							"updated_at": time.Now(),
						})

						// Filter out this candidate and try the next leasable one
						var next []QueuedTask
						for _, c := range candidates {
							if c.ID != candidate.ID {
								next = append(next, c)
							}
						}
						candidates = next
						continue
					}
				}
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
					"started_at":       now,
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
		}
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
		// Failed-dependency policy (RFC §3): a task can never satisfy its DAG once
		// one of its dependencies has FAILED, so fail it fast rather than leaving
		// it QUEUED forever. This cascades transitively — a task failed here becomes
		// a failed dependency for its own dependents on the next lease scan.
		if dependencyFailed(&candidates[i], statuses) {
			reason := "a dependency failed; task cannot run"
			if err := tx.Model(&QueuedTask{}).
				Where("id = ? AND status = ?", candidates[i].ID, TaskQueued).
				Updates(map[string]interface{}{
					"status":        TaskFailed,
					"result_detail": &reason,
					"updated_at":    time.Now(),
				}).Error; err != nil {
				return nil, err
			}
			continue
		}
		if dependenciesSatisfied(&candidates[i], statuses) {
			return &candidates[i], nil
		}
	}
	return nil, nil
}

// dependencyFailed reports whether any of t's plan dependencies has FAILED.
func dependencyFailed(t *QueuedTask, statuses map[string]string) bool {
	for _, id := range dependencyTaskIDs(t) {
		if statuses[id] == TaskFailed {
			return true
		}
	}
	return false
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

func (s *PostgresStore) CompleteTask(ctx context.Context, taskID, leaseID, finalStatus, resultURL, detail string) (bool, error) {
	if finalStatus != TaskSucceeded && finalStatus != TaskFailed {
		return false, fmt.Errorf("invalid final status %q (want %s or %s)", finalStatus, TaskSucceeded, TaskFailed)
	}

	var rowsAffected int64
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		now := time.Now()
		updates := map[string]interface{}{
			"status":     finalStatus,
			"updated_at": now,
		}
		if resultURL == "" {
			updates["result_url"] = nil
		} else {
			updates["result_url"] = resultURL
		}
		if detail == "" {
			updates["result_detail"] = nil
		} else {
			updates["result_detail"] = detail
		}

		var t QueuedTask
		if err := tx.Select("job_id", "started_at").First(&t, "id = ?", taskID).Error; err != nil {
			return err
		}

		res := tx.Model(&QueuedTask{}).
			Where("id = ? AND lease_id = ? AND status = ?", taskID, leaseID, TaskLeased).
			Updates(updates)
		if res.Error != nil {
			return res.Error
		}
		rowsAffected = res.RowsAffected

		if rowsAffected > 0 && t.JobID != "" {
			// Meter from StartedAt (set once at lease), not UpdatedAt: RenewLease
			// bumps UpdatedAt every renewal, so a task longer than the renew
			// interval would otherwise record only the time since its last renew.
			if t.StartedAt != nil {
				elapsed := time.Since(*t.StartedAt).Minutes()
				if elapsed > 0 {
					tx.Model(&Job{}).Where("id = ?", t.JobID).UpdateColumn("agent_minutes", gorm.Expr("agent_minutes + ?", elapsed))
				}
			}
			if finalStatus == TaskFailed {
				failJobAndQueuedTasks(tx, t.JobID, "Sibling task failed")
			}
		}
		return nil
	})

	if err != nil {
		return false, err
	}
	return rowsAffected > 0, nil
}

func failJobAndQueuedTasks(tx *gorm.DB, jobID, reason string) {
	now := time.Now()
	tx.Model(&QueuedTask{}).Where("job_id = ? AND status = ?", jobID, TaskQueued).Updates(map[string]interface{}{
		"status":        TaskFailed,
		"result_detail": &reason,
		"updated_at":    now,
	})
	tx.Model(&Job{}).Where("id = ?", jobID).Updates(map[string]interface{}{
		"status":     "FAILED",
		"error":      &reason,
		"updated_at": now,
	})
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
		var poisonTasks []QueuedTask
		if err := tx.Where("status = ? AND lease_expires_at < ? AND attempts >= ?", TaskLeased, now, MaxLeaseAttempts).Find(&poisonTasks).Error; err != nil {
			return err
		}

		if len(poisonTasks) > 0 {
			if err := tx.Model(&QueuedTask{}).
				Where("status = ? AND lease_expires_at < ? AND attempts >= ?", TaskLeased, now, MaxLeaseAttempts).
				Updates(map[string]interface{}{
					"status":     TaskFailed,
					"updated_at": now,
				}).Error; err != nil {
				return err
			}
			for _, pt := range poisonTasks {
				if pt.JobID != "" {
					failJobAndQueuedTasks(tx, pt.JobID, "Task dead-lettered")
				}
			}
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

// ExpireStaleQueuedTasks fails any task that has waited in QUEUED longer than
// ttl — e.g. no fleet ever connected to lease it — so work doesn't hang in the
// queue forever. It fails the task's job too, mirroring the dead-letter path.
// Returns the number of tasks expired. A non-positive ttl disables the sweep.
// Run this periodically as a sweeper.
func (s *PostgresStore) ExpireStaleQueuedTasks(ctx context.Context, ttl time.Duration) (int, error) {
	if ttl <= 0 {
		return 0, nil
	}
	now := time.Now()
	cutoff := now.Add(-ttl)

	var expired int
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var stale []QueuedTask
		if err := tx.Where("status = ? AND created_at < ?", TaskQueued, cutoff).Find(&stale).Error; err != nil {
			return err
		}
		if len(stale) == 0 {
			return nil
		}

		detail := fmt.Sprintf("timed out after %s waiting in the queue (no fleet available to run it)", ttl)
		res := tx.Model(&QueuedTask{}).
			Where("status = ? AND created_at < ?", TaskQueued, cutoff).
			Updates(map[string]interface{}{
				"status":        TaskFailed,
				"result_detail": detail,
				"updated_at":    now,
			})
		if res.Error != nil {
			return res.Error
		}
		expired = int(res.RowsAffected)

		for _, st := range stale {
			if st.JobID != "" {
				failJobAndQueuedTasks(tx, st.JobID, "Task timed out in queue")
			}
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return expired, nil
}

// GetJobTasks retrieves all tasks for a given job, scoped by orgID and ordered by creation time.
func (s *PostgresStore) GetJobTasks(ctx context.Context, orgID, jobID string) ([]QueuedTask, error) {
	var tasks []QueuedTask
	err := s.db.WithContext(ctx).
		Where("org_id = ? AND job_id = ?", orgID, jobID).
		Order("created_at ASC").
		Find(&tasks).Error
	return tasks, err
}

func (s *PostgresStore) ListJobs(ctx context.Context, orgID string) ([]JobSummary, error) {
	var tasks []QueuedTask
	if err := s.db.WithContext(ctx).Where("org_id = ? AND job_id != ''", orgID).Find(&tasks).Error; err != nil {
		return nil, err
	}

	type jobAgg struct {
		JobID     string
		CreatedAt time.Time
		TaskCount int
		Failed    int
		Succeeded int
		Leased    int
		PRURLs    []string
		Task      string
		Repo      string
		FleetID   string
		DaemonID  string
	}

	jobMap := make(map[string]*jobAgg)
	for _, t := range tasks {
		agg, ok := jobMap[t.JobID]
		if !ok {
			agg = &jobAgg{JobID: t.JobID, CreatedAt: t.CreatedAt}
			jobMap[t.JobID] = agg
		}
		agg.TaskCount++
		if t.CreatedAt.After(agg.CreatedAt) {
			agg.CreatedAt = t.CreatedAt
		}
		// The overall goal + repo live on the task spec. Prefer the job-level
		// "job_task" the planner stamps; fall back to the worker task text.
		if agg.Task == "" {
			if jt, ok := t.Spec["job_task"].(string); ok && jt != "" {
				agg.Task = jt
			} else if wt, ok := t.Spec["task"].(string); ok {
				agg.Task = wt
			}
		}
		if agg.Repo == "" {
			if ru, ok := t.Spec["repo_url"].(string); ok {
				agg.Repo = shortRepo(ru)
			}
		}
		// Executor linkage: the fleet the work targets, and the daemon that
		// leased it (retained after completion). First non-empty wins.
		if agg.FleetID == "" && t.FleetID != "" {
			agg.FleetID = t.FleetID
		}
		if agg.DaemonID == "" && t.LeasedBy != nil && *t.LeasedBy != "" {
			agg.DaemonID = *t.LeasedBy
		}
		if t.Status == TaskFailed {
			agg.Failed++
		}
		if t.Status == TaskSucceeded {
			agg.Succeeded++
		}
		if t.Status == TaskLeased {
			agg.Leased++
		}
		if t.ResultURL != nil && *t.ResultURL != "" {
			agg.PRURLs = append(agg.PRURLs, *t.ResultURL)
		}
	}

	var summaries []JobSummary
	for _, agg := range jobMap {
		status := "QUEUED"
		if agg.Failed > 0 {
			status = "FAILED"
		} else if agg.Succeeded == agg.TaskCount {
			status = "SUCCEEDED"
		} else if agg.Leased > 0 {
			status = "RUNNING"
		}

		prUrls := agg.PRURLs
		if prUrls == nil {
			prUrls = []string{}
		}

		summaries = append(summaries, JobSummary{
			JobID:     agg.JobID,
			CreatedAt: agg.CreatedAt,
			TaskCount: agg.TaskCount,
			Status:    status,
			PRURLs:    prUrls,
			Task:      agg.Task,
			Repo:      agg.Repo,
			FleetID:   agg.FleetID,
			DaemonID:  agg.DaemonID,
		})
	}

	sort.Slice(summaries, func(i, j int) bool {
		return summaries[i].CreatedAt.After(summaries[j].CreatedAt)
	})

	if summaries == nil {
		summaries = []JobSummary{}
	}

	return summaries, nil
}
