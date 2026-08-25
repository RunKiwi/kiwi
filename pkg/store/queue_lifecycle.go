package store

import (
	"context"
	"log/slog"
	"sort"
	"time"

	"gorm.io/gorm"
)

// CancelJob stops a job: every task of it that is not already terminal moves to
// CANCELLED. Returns how many tasks were affected.
//
// A QUEUED task stops immediately — nothing will ever lease it again. A LEASED
// task is a different matter: the daemon executing it is a separate process we
// cannot reach, so cancellation here is a *revocation of its lease*, not a kill.
// The daemon discovers this on its next RenewLease, which now fails with 409
// because the task is no longer LEASED, and aborts its run. Until then it keeps
// working; the fencing token means the result it eventually reports is rejected
// (CompleteTask requires status = LEASED), so a cancelled task can never be
// resurrected by a late completion.
//
// Scoped by org: another tenant's job id is a no-op, not an error, so this
// cannot be used to probe which job ids exist.
func (s *PostgresStore) CancelJob(ctx context.Context, orgID, jobID, reason string) (int, error) {
	if reason == "" {
		reason = "cancelled by user"
	}
	now := time.Now()

	res := s.db.WithContext(ctx).Model(&QueuedTask{}).
		// PLAN_REVIEW is included: a job parked in Plan Mode review has no
		// QUEUED or LEASED task, only this one, and without it a plan a human
		// never approves nor rejects could never be cancelled either.
		Where("org_id = ? AND job_id = ? AND status IN ?", orgID, jobID, []string{TaskQueued, TaskLeased, TaskPlanReview}).
		Updates(map[string]interface{}{
			"status": TaskCancelled,
			// Clear the lease so the expiry sweeper cannot pick this row up and
			// requeue it — RequeueExpiredLeases keys off LEASED, but leaving a
			// stale fencing token on a terminal row invites confusion.
			"leased_by":        nil,
			"lease_id":         nil,
			"lease_expires_at": nil,
			"result_detail":    reason,
			"updated_at":       now,
		})
	if res.Error != nil {
		return 0, res.Error
	}

	if res.RowsAffected > 0 {
		s.db.WithContext(ctx).Model(&Job{}).Where("id = ? AND org_id = ?", jobID, orgID).
			Updates(map[string]interface{}{
				"status": "CANCELLED",
				// Reset even for a job that was never in Plan Mode (already ""):
				// otherwise a job cancelled mid plan-review keeps plan_status
				// "pending_review" forever, and the dashboard's PlanApprovalCard
				// checks plan_status alone, not job status, to decide whether to
				// render.
				"plan_status": "",
				"updated_at":  now,
			})
	}
	return int(res.RowsAffected), nil
}

// RetryJob returns a job's unsuccessful tasks to the queue so they run again.
// Returns how many tasks were requeued.
//
// Only FAILED and CANCELLED tasks are retried. A SUCCEEDED task is left alone —
// re-running it would redo work that already produced a PR — and QUEUED/LEASED
// tasks are already on their way. Attempts is reset to zero: the retry is a
// deliberate human act, and carrying the old count forward would let the
// poison-pill guard (MaxLeaseAttempts) dead-letter the task almost immediately.
//
// The previous result is cleared rather than kept, so a stale failure reason or
// PR link cannot be read as belonging to the new run.
func (s *PostgresStore) RetryJob(ctx context.Context, orgID, jobID string) (int, error) {
	now := time.Now()

	var affected int64
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// Only the newest attempt in each thread comes back.
		//
		// A job used to hold one task per planned worker, so retrying it meant
		// retrying all of them. Since a review comment can continue a task, a
		// job can also hold a thread — and re-queueing a thread's original task
		// alongside its failed continuations would put several tasks on one
		// branch, all force-pushing to it, with only one able to hold the
		// session. That is the hazard ActiveTaskInThread exists to prevent, and
		// a retry must not walk around it.
		ids, err := retryableTaskIDs(tx, orgID, jobID)
		if err != nil {
			return err
		}
		if len(ids) == 0 {
			return nil
		}

		res := tx.Model(&QueuedTask{}).
			Where("org_id = ? AND id IN ?", orgID, ids).
			Updates(map[string]interface{}{
				"status":           TaskQueued,
				"leased_by":        nil,
				"lease_id":         nil,
				"lease_expires_at": nil,
				"started_at":       nil,
				"attempts":         0,
				"result_url":       nil,
				"result_detail":    nil,
				"created_at":       now,
				"updated_at":       now,
			})
		if res.Error != nil {
			return res.Error
		}
		affected = res.RowsAffected

		if affected > 0 {
			// The job is running again, so its terminal status and error must not
			// linger — the dashboard reads the job row, and a QUEUED task under a
			// FAILED job is a contradiction.
			tx.Model(&Job{}).Where("id = ? AND org_id = ?", jobID, orgID).
				Updates(map[string]interface{}{"status": "PENDING", "error": nil, "updated_at": now})
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return int(affected), nil
}

// DeleteJob removes a job's tasks so it disappears from the dashboard. Returns
// how many tasks were deleted.
//
// It deletes only the queue rows (and the job row), NOT the job's execution
// record. Those records are hash-chained per org via prev_record_hash — deleting
// a link would break the chain's verifiability for every record after it, which
// is the one property the provenance feature exists to provide. A deleted job
// therefore leaves its attestation behind by design.
//
// Cancels nothing: a LEASED task whose row is deleted leaves its daemon working
// with no row to report against. Callers should cancel first; CancelJob then
// DeleteJob is the safe order, and the HTTP handler enforces it.
func (s *PostgresStore) DeleteJob(ctx context.Context, orgID, jobID string) (int, error) {
	var deleted int64
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		res := tx.Where("org_id = ? AND job_id = ?", orgID, jobID).Delete(&QueuedTask{})
		if res.Error != nil {
			return res.Error
		}
		deleted = res.RowsAffected

		return tx.Where("id = ? AND org_id = ?", jobID, orgID).Delete(&Job{}).Error
	})
	if err != nil {
		return 0, err
	}

	// The job's learning row is ancillary — it feeds future planning, nothing the
	// user can see. Removing it is best-effort and deliberately outside the
	// transaction above: a learnings table that is missing (BYOC without
	// pgvector) or unhappy must not be able to block deleting a job, which is the
	// part the user actually asked for.
	if err := s.db.WithContext(ctx).
		Where("org_id = ? AND job_id = ?", orgID, jobID).
		Delete(&JobLearning{}).Error; err != nil {
		slog.Warn("delete job learnings", "org", orgID, "job", jobID, "err", err)
	}

	return int(deleted), nil
}

// HasActiveTasks reports whether an org has any task still QUEUED or LEASED.
// Used by the fleet-host autoscaler to decide whether it is safe to stop a
// machine, and by DeleteJob callers to decide whether a cancel must come first.
func (s *PostgresStore) HasActiveTasks(ctx context.Context, orgID string) (bool, error) {
	var n int64
	q := s.db.WithContext(ctx).Model(&QueuedTask{}).
		Where("status IN ?", []string{TaskQueued, TaskLeased})
	if orgID != "" {
		q = q.Where("org_id = ?", orgID)
	}
	if err := q.Count(&n).Error; err != nil {
		return false, err
	}
	return n > 0, nil
}

// RecordTaskProgress stores what a daemon says it is doing right now.
//
// Guarded by the fencing token, exactly as CompleteTask and RenewLease are: a
// daemon whose lease was reassigned must not be able to write progress onto a
// task another daemon now owns, or the dashboard would show one run's output
// under another's. It also refuses to touch a task that is no longer LEASED, so
// a late update cannot overwrite a finished task's final state.
//
// Returns false when the write did not apply, which the caller treats as
// informational — progress is best-effort and must never fail a run.
func (s *PostgresStore) RecordTaskProgress(ctx context.Context, taskID, leaseID, phase, output string, phaseSince time.Time) (bool, error) {
	now := time.Now()
	updates := map[string]interface{}{
		"progress_at": &now,
	}
	if phase != "" {
		updates["progress_phase"] = &phase
	}
	if output != "" {
		updates["progress_output"] = &output
	}
	// A zero PhaseSince means the caller has nothing new to say about timing
	// (e.g. an output-only update on an unchanged phase) — leaving the column
	// untouched keeps the previously recorded start time, rather than the
	// write racing it back to NULL.
	if !phaseSince.IsZero() {
		updates["progress_phase_since"] = &phaseSince
	}

	res := s.db.WithContext(ctx).
		Model(&QueuedTask{}).
		Where("id = ? AND lease_id = ? AND status = ?", taskID, leaseID, TaskLeased).
		Updates(updates)
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
}

// retryableTaskIDs picks the task to re-queue for each thread in a job: the
// newest failed or cancelled one, which is the attempt a user retrying the job
// means.
//
// Separate workers of a plain job are separate threads, so a job that never
// had a continuation retries every one of them exactly as it always did.
func retryableTaskIDs(tx *gorm.DB, orgID, jobID string) ([]string, error) {
	var tasks []QueuedTask
	if err := tx.Select("id", "root_task_id", "created_at").
		Where("org_id = ? AND job_id = ? AND status IN ?", orgID, jobID, []string{TaskFailed, TaskCancelled}).
		Order("created_at asc, id asc").
		Find(&tasks).Error; err != nil {
		return nil, err
	}

	newest := make(map[string]QueuedTask, len(tasks))
	for _, task := range tasks {
		root := task.RootTaskID
		if root == "" {
			root = task.ID
		}
		// Ordered oldest-first above, so the last write per thread wins.
		newest[root] = task
	}

	ids := make([]string, 0, len(newest))
	for _, task := range newest {
		ids = append(ids, task.ID)
	}
	sort.Strings(ids)
	return ids, nil
}
