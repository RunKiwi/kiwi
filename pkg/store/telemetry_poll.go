package store

import (
	"context"
	"time"
)

func (s *PostgresStore) CreateTelemetryPoll(ctx context.Context, p *PostMergeTelemetryPoll) error {
	return s.db.WithContext(ctx).Create(p).Error
}

// ClaimDuePolls follows the same conditional-UPDATE-then-check-RowsAffected
// idiom FinalizeMonitor uses for its single-fire guard — GORM has no native
// batch UPDATE...RETURNING in this codebase, so candidates are selected
// first, then each is claimed individually with the due-and-unclaimed guard
// re-checked in the UPDATE's WHERE clause. This is what makes a claim safe
// even if more than one daemon process somehow serves the same org: only one
// UPDATE can win the row.
func (s *PostgresStore) ClaimDuePolls(ctx context.Context, orgID string, now time.Time, limit int) ([]PostMergeTelemetryPoll, error) {
	var candidates []PostMergeTelemetryPoll
	if err := s.db.WithContext(ctx).
		Where("org_id = ? AND next_poll_at <= ? AND claimed_at IS NULL", orgID, now).
		Order("next_poll_at").
		Limit(limit).
		Find(&candidates).Error; err != nil {
		return nil, err
	}

	claimed := make([]PostMergeTelemetryPoll, 0, len(candidates))
	for _, c := range candidates {
		res := s.db.WithContext(ctx).Model(&PostMergeTelemetryPoll{}).
			Where("id = ? AND claimed_at IS NULL", c.ID).
			Update("claimed_at", now)
		if res.Error != nil {
			return nil, res.Error
		}
		if res.RowsAffected > 0 {
			claimed = append(claimed, c)
		}
	}
	return claimed, nil
}

// RecordPollResult persists the latest query result and either reschedules
// the poll (reschedule=true, the common case — advance next_poll_at and
// clear claimed_at so the next due-check can pick it up) or leaves it
// unclaimed-but-never-due-again (reschedule=false — the monitor finalized
// from this result, e.g. a REGRESSION, and there is no next poll to run).
func (s *PostgresStore) RecordPollResult(ctx context.Context, pollID string, next time.Time, resultJSON string, reschedule bool) error {
	updates := map[string]interface{}{
		"last_result": resultJSON,
		"updated_at":  time.Now(),
	}
	if reschedule {
		updates["claimed_at"] = nil
		updates["next_poll_at"] = next
	} else {
		updates["claimed_at"] = nil
		updates["next_poll_at"] = next.Add(24 * 365 * time.Hour) // effectively never — this poll is done
	}
	return s.db.WithContext(ctx).Model(&PostMergeTelemetryPoll{}).
		Where("id = ?", pollID).
		Updates(updates).Error
}

// ReleaseStalePolls clears claimed_at on any poll claimed before olderThan
// and never reported back — the daemon that claimed it may have crashed or
// lost connectivity mid-query. Releasing it lets the next due-check retry
// it rather than leaving it claimed forever.
func (s *PostgresStore) ReleaseStalePolls(ctx context.Context, olderThan time.Time) (int64, error) {
	res := s.db.WithContext(ctx).Model(&PostMergeTelemetryPoll{}).
		Where("claimed_at IS NOT NULL AND claimed_at < ?", olderThan).
		Update("claimed_at", nil)
	return res.RowsAffected, res.Error
}
