package store

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ErrSessionNotFound reports that no session exists for a task — the normal
// answer on a task's first lease, not a failure.
var ErrSessionNotFound = errors.New("agent session not found")

// GetAgentSessionByTask loads the session for a task, if one exists.
func (s *PostgresStore) GetAgentSessionByTask(ctx context.Context, orgID, taskID string) (*AgentSession, error) {
	var sess AgentSession
	err := s.db.WithContext(ctx).
		Where("org_id = ? AND task_id = ?", orgID, taskID).
		First(&sess).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrSessionNotFound
	}
	if err != nil {
		return nil, err
	}
	return &sess, nil
}

// SaveAgentSession creates or updates a session's checkpoint and appends any
// new events, in one transaction.
//
// The two must land together. A checkpoint that advanced the round without its
// events would leave a session whose history has a hole exactly where the
// interesting part is; events without the checkpoint would replay work already
// done. Since the whole point is surviving a process that dies at an arbitrary
// moment, "these two writes are usually both fine" is not good enough.
//
// Events conflicting on (session, round, seq) are ignored rather than rejected:
// a daemon that checkpointed successfully but lost the response will retry, and
// that retry must be a no-op rather than an error that fails a healthy run.
func (s *PostgresStore) SaveAgentSession(ctx context.Context, sess *AgentSession, events []AgentSessionEvent) error {
	if sess == nil || sess.ID == "" || sess.OrgID == "" || sess.TaskID == "" {
		return errors.New("agent session needs an id, org and task")
	}
	now := time.Now()
	sess.UpdatedAt = now
	if sess.Status == "" {
		sess.Status = SessionRunning
	}

	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "id"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"head_sha", "phase", "round", "round_attempts", "rejections",
				"state", "cost_usd", "tokens_in", "tokens_out", "status", "updated_at",
			}),
		}).Create(sess).Error; err != nil {
			return err
		}
		if len(events) == 0 {
			return nil
		}
		for i := range events {
			events[i].SessionID = sess.ID
			events[i].OrgID = sess.OrgID
			if events[i].CreatedAt.IsZero() {
				events[i].CreatedAt = now
			}
		}
		return tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&events).Error
	})
}

// ListAgentSessionEvents returns a session's history in order.
func (s *PostgresStore) ListAgentSessionEvents(ctx context.Context, orgID, sessionID string) ([]AgentSessionEvent, error) {
	var events []AgentSessionEvent
	err := s.db.WithContext(ctx).
		Where("org_id = ? AND session_id = ?", orgID, sessionID).
		Order("round ASC, seq ASC").
		Find(&events).Error
	return events, err
}

// FinishAgentSession records a session's terminal status. It is separate from
// SaveAgentSession because a finished session must stop being resumable even if
// its task is leased again — a retry is a new session, not a continuation of a
// concluded one.
func (s *PostgresStore) FinishAgentSession(ctx context.Context, orgID, sessionID, status string) error {
	if status != SessionSucceeded && status != SessionFailed {
		return errors.New("agent session status must be SUCCEEDED or FAILED")
	}
	return s.db.WithContext(ctx).Model(&AgentSession{}).
		Where("org_id = ? AND id = ?", orgID, sessionID).
		Updates(map[string]interface{}{"status": status, "updated_at": time.Now()}).Error
}
