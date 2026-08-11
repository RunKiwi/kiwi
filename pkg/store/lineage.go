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
)

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
