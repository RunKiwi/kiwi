package store

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
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

// LeaseNextTask atomically claims the oldest QUEUED task for orgID, marking it
// LEASED to leasedBy with a fresh fencing LeaseID for ttl. It returns
// (nil, nil) when no task is available.
//
// On Postgres the candidate row is selected FOR UPDATE SKIP LOCKED so
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
		q := tx.Where("org_id = ? AND status = ?", orgID, TaskQueued).Order("created_at ASC")
		if tx.Dialector.Name() == "postgres" {
			q = q.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"})
		}

		var candidate QueuedTask
		if err := q.First(&candidate).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil // no work available
			}
			return err
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

		if err := tx.First(&candidate, "id = ?", candidate.ID).Error; err != nil {
			return err
		}
		leased = &candidate
		return nil
	})
	return leased, err
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
