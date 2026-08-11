package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
)

// ErrLeaseNotHeld means the caller does not currently hold the task's lease:
// wrong fencing token, task already finished, or the lease lapsed and was
// handed to somebody else.
var ErrLeaseNotHeld = errors.New("lease not held")

// FindLeasedTask returns a task only to the holder of its current lease.
//
// This is an authorisation check, not a lookup with a filter bolted on. It
// backs the git-token endpoint, where holding the lease is the entire basis for
// being allowed to mint a credential against the org's repository. Comparing
// the fencing token is what stops a daemon that has lost its lease, or one
// replaying another daemon's task id, from continuing to buy tokens.
//
// The expiry comparison is deliberate rather than relying on Status alone: a
// lapsed lease is reassigned by a sweep, and between lapse and sweep the row
// still reads LEASED with the old holder's token.
func (s *PostgresStore) FindLeasedTask(ctx context.Context, taskID, leaseID string) (*QueuedTask, error) {
	if taskID == "" || leaseID == "" {
		return nil, ErrLeaseNotHeld
	}

	var task QueuedTask
	err := s.db.WithContext(ctx).
		Where("id = ? AND lease_id = ? AND status = ?", taskID, leaseID, "LEASED").
		First(&task).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrLeaseNotHeld
	}
	if err != nil {
		return nil, fmt.Errorf("find leased task: %w", err)
	}

	if task.LeaseExpiresAt == nil || task.LeaseExpiresAt.Before(time.Now().UTC()) {
		return nil, ErrLeaseNotHeld
	}
	return &task, nil
}
