package store

import (
	"context"
	"fmt"
	"time"

	"gorm.io/gorm"
)

// ReattachSession moves a session to the task that is about to continue it,
// and reopens it.
//
// A session belongs to exactly one task at a time — agent_sessions.task_id is
// uniquely indexed, and handleDaemonSessionLoad resolves a session by that
// column rather than by session id. So the session travelling along a thread
// is not a trick: it is the only representation the load path can find.
//
// Reopening happens here and nowhere else. cpSessionStore.Load refuses to
// resume a session whose status is not RUNNING, which is what stops an
// ordinary re-lease of a finished task from handing the Architect a history
// ending in a verdict it already gave. Keeping the flip in this one deliberate
// call means a continuation is the only thing in the system that can undo it.
//
// A missing session is an error rather than a no-op. The consequence of
// silence is a continuation that starts from round zero: the work looks like
// it ran, the pull request comes out wrong, and nothing anywhere explains it.
func (s *PostgresStore) ReattachSession(ctx context.Context, orgID, sessionID, newTaskID string) error {
	return ReattachSessionIn(s.db.WithContext(ctx), orgID, sessionID, newTaskID)
}

// ReattachSessionIn is ReattachSession against a caller's transaction.
//
// It exists because the move has to be atomic with enqueueing the task that
// will run it: a task without its session starts from round zero with the
// history discarded, and a session without its task strands the thread on a
// task that will never be leased. Calling the method above from inside another
// transaction would use a different connection — which is not merely
// non-atomic but deadlocks outright on SQLite, where the test suite found it.
func ReattachSessionIn(tx *gorm.DB, orgID, sessionID, newTaskID string) error {
	if orgID == "" || sessionID == "" || newTaskID == "" {
		return fmt.Errorf("reattach session: org, session and task are all required")
	}

	res := tx.Model(&AgentSession{}).
		Where("id = ? AND org_id = ?", sessionID, orgID).
		Updates(map[string]interface{}{
			"task_id":    newTaskID,
			"status":     SessionRunning,
			"updated_at": time.Now().UTC(),
		})
	if res.Error != nil {
		return fmt.Errorf("reattach session %s to task %s: %w", sessionID, newTaskID, res.Error)
	}
	if res.RowsAffected == 0 {
		return fmt.Errorf("reattach session %s: no such session for org %s", sessionID, orgID)
	}
	return nil
}
