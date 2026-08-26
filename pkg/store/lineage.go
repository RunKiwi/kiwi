package store

import (
	"context"
	"errors"

	"gorm.io/gorm"
)

// Task lineage: how one task relates to the task it came from.
//
// A task used to be the whole story — submitted, run, delivered as a pull
// request, done. A review comment on that pull request now starts another
// task that continues the same session on the same branch, so "what happened
// here" is a thread rather than a row, and the dashboard has to be able to ask
// for one.
//
// The shape is a tree rather than a list, because forking any task is next and
// a fork is simply a second child of the same parent. Choosing a list now
// would mean migrating the moment that lands.

// How a task came to exist.
const (
	OriginSubmit    = "submit"
	OriginPRComment = "pr_comment"
	OriginFork      = "fork"
	// OriginPlanApproved marks a continuation created by approving a Plan
	// Mode review. It reuses the same task-lineage mechanism as OriginPRComment
	// (ParentTaskID/RootTaskID) so the daemon resumes the exact session that
	// was paused, rather than starting a fresh one.
	OriginPlanApproved = "plan_approved"
	// OriginPlanRevision marks a continuation created by rejecting a Plan
	// Mode review with feedback. Same lineage mechanism as OriginPlanApproved
	// — same SessionID, same worker spec — except the daemon re-plans (see
	// pkg/session.Task.RevisionFeedback) instead of resuming the round loop.
	OriginPlanRevision = "plan_revision"
	// OriginSlack marks any task that started from Slack — a fresh @mention,
	// a "new" unrelated task started from an existing thread, or a
	// continuation of one. A fork started from Slack still gets OriginFork,
	// not this — a fork's defining fact is that it shares its parent's
	// branch, which matters more than which surface asked for it, and
	// OriginFork already carries the lineage a UI needs to render that.
	// OriginSlack is distinct from OriginPRComment (a GitHub PR review
	// comment), which a Slack-triggered continuation is not, even though
	// buildContinuationTask treats both the same way otherwise. The frontend
	// badges the two differently, so the label is not merely cosmetic.
	OriginSlack = "slack"
	// OriginPostMergeRemediation marks a continuation task auto-spawned by a
	// Post-Merge Verification REGRESSION verdict, not a real PR comment — it
	// has no GitHub comment behind it, so it must not be labeled
	// OriginPRComment on the dashboard.
	OriginPostMergeRemediation = "postmerge_remediation"
)

// BeforeCreate gives every task a thread, whichever path created it.
//
// This is a hook rather than a few lines in EnqueueTask because EnqueueTask is
// not the path that matters: SubmitPlan builds its tasks with tx.Create inside
// the transaction that writes the manifest, so defaulting in the store helper
// alone would have left every real task with no root — and every lineage read
// returning nothing for it, which reads as "this task never happened".
//
// A task with no parent is the root of its own thread. That is true of every
// ordinary submission and of every task written before lineage existed, so it
// is the right default rather than a special case.
func (t *QueuedTask) BeforeCreate(*gorm.DB) error {
	if t.Origin == "" {
		t.Origin = OriginSubmit
	}
	if t.RootTaskID == "" {
		t.RootTaskID = t.ID
	}
	return nil
}

// ThreadTasks returns every task in a thread, oldest first.
//
// One indexed read on root_task_id rather than a walk up parent pointers: this
// is asked on every task view, and a recursive query for a chain that is
// almost always two or three rows long would be machinery without a purpose.
func (s *PostgresStore) ThreadTasks(ctx context.Context, orgID, rootTaskID string) ([]QueuedTask, error) {
	if orgID == "" || rootTaskID == "" {
		return nil, nil
	}
	var tasks []QueuedTask
	if err := s.db.WithContext(ctx).
		Where("org_id = ? AND root_task_id = ?", orgID, rootTaskID).
		Order("created_at asc, id asc").
		Find(&tasks).Error; err != nil {
		return nil, err
	}
	return tasks, nil
}

// BatchThreadTasks returns every task across all given root task IDs in a single
// query, avoiding N+1 when checking multiple threads at once (e.g. velocity analytics).
func (s *PostgresStore) BatchThreadTasks(ctx context.Context, orgID string, rootTaskIDs []string) ([]QueuedTask, error) {
	if orgID == "" || len(rootTaskIDs) == 0 {
		return nil, nil
	}
	var tasks []QueuedTask
	if err := s.db.WithContext(ctx).
		Where("org_id = ? AND root_task_id IN ?", orgID, rootTaskIDs).
		Order("root_task_id asc, created_at asc, id asc").
		Find(&tasks).Error; err != nil {
		return nil, err
	}
	return tasks, nil
}

// ActiveTaskInThread returns the thread's unfinished task, or nil when every
// task in it has concluded.
//
// This is what stops a second review comment buying a second concurrent round.
// Two tasks in one thread share a job id and therefore a branch, and both
// force-push to it — so the loser's work disappears with no error raised
// anywhere, which is the worst way for work to be lost.
func (s *PostgresStore) ActiveTaskInThread(ctx context.Context, orgID, rootTaskID string) (*QueuedTask, error) {
	if orgID == "" || rootTaskID == "" {
		return nil, nil
	}
	var task QueuedTask
	err := s.db.WithContext(ctx).
		Where("org_id = ? AND root_task_id = ? AND status IN ?",
			orgID, rootTaskID, []string{TaskQueued, TaskLeased}).
		Order("created_at asc").
		First(&task).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &task, nil
}
