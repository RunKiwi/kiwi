package store

import (
	"context"
	"time"
)

// CreateMonitor inserts a new PostMergeMonitor row. Called once per merged
// job, by the merge webhook handler — the (org_id, job_id) unique index
// (migration 0037) makes a second call for the same job a constraint error
// rather than a silent duplicate.
func (s *PostgresStore) CreateMonitor(ctx context.Context, m *PostMergeMonitor) error {
	return s.db.WithContext(ctx).Create(m).Error
}

// GetMonitorByMergeCommit finds the still-open monitor for a merge commit, if
// any. Used by revert-PR and check-run detection to resolve a webhook event
// back to the monitor it should finalize. Only a MONITORING row matches — a
// monitor that already finalized (e.g. two check runs fail; the first one
// already flipped it to REGRESSION) must not be findable again, since
// finalizeMonitor's caller in ee/orchestrator relies on "not found" to mean
// "nothing left to do here."
func (s *PostgresStore) GetMonitorByMergeCommit(ctx context.Context, orgID, sha string) (*PostMergeMonitor, error) {
	var m PostMergeMonitor
	err := s.db.WithContext(ctx).
		Where("org_id = ? AND merge_commit_sha = ? AND status = ?", orgID, sha, MonitorStatusMonitoring).
		First(&m).Error
	if err != nil {
		return nil, err
	}
	return &m, nil
}

// FinalizeMonitor atomically transitions a monitor from MONITORING to a
// terminal status, recording the evidence that justified it. The bool return
// is the single-fire guard: true only for the caller whose UPDATE actually
// matched a MONITORING row, exactly as CompleteTask's lease-id-guarded UPDATE
// (pkg/store/queue.go) guards against two callers completing the same task.
// A revert-PR event, a failed check-run event, and the window-elapsed sweep
// can all race to finalize the same monitor; only the first one's verdict
// and evidence stick, and only that caller may go on to submit a remediation
// continuation.
func (s *PostgresStore) FinalizeMonitor(ctx context.Context, id, newStatus, evidence string) (bool, error) {
	now := time.Now()
	res := s.db.WithContext(ctx).Model(&PostMergeMonitor{}).
		Where("id = ? AND status = ?", id, MonitorStatusMonitoring).
		Updates(map[string]interface{}{
			"status":           newStatus,
			"verdict_evidence": evidence,
			"finalized_at":     now,
			"updated_at":       now,
		})
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
}

// SetMonitorRemediationTaskID records which continuation task a REGRESSION
// verdict spawned, for dashboard display. Called after FinalizeMonitor has
// already won the single-fire race, so this is a second, non-racing update.
func (s *PostgresStore) SetMonitorRemediationTaskID(ctx context.Context, id, taskID string) error {
	return s.db.WithContext(ctx).Model(&PostMergeMonitor{}).
		Where("id = ?", id).
		Update("remediation_task_id", taskID).Error
}

// ListMonitorsPastWindow returns MONITORING monitors whose window has
// elapsed — candidates for the periodic sweep to finalize as VERIFIED (no
// bad signal arrived in time).
//
// Capped at 200 rows and intentionally not exhaustive per call: after a long
// orchestrator outage the backlog could be large, and finalizing each one
// makes a PR comment API call, so an unbounded sweep could turn the first
// tick after restart into one very long call. The remainder isn't lost —
// the next 5-minute tick re-runs this same query, which still matches every
// row still in MONITORING, so the backlog drains over a few ticks instead
// of one.
func (s *PostgresStore) ListMonitorsPastWindow(ctx context.Context, now time.Time) ([]PostMergeMonitor, error) {
	var out []PostMergeMonitor
	err := s.db.WithContext(ctx).
		Where("status = ? AND window_ends_at <= ?", MonitorStatusMonitoring, now).
		Limit(200).
		Find(&out).Error
	return out, err
}
