package store

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCompleteTaskAcceptsPlanReviewStatus(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	task := &QueuedTask{ID: "t-plan-1", OrgID: "org-1", JobID: "job-1", Status: TaskQueued, Spec: map[string]interface{}{}}
	require.NoError(t, s.EnqueueTask(ctx, task))
	leased, err := s.LeaseNextTask(ctx, "org-1", "daemon-1", "", time.Minute)
	require.NoError(t, err)
	require.NotNil(t, leased)

	ok, err := s.CompleteTask(ctx, TaskCompletion{
		TaskID: "t-plan-1", LeaseID: *leased.LeaseID, FinalStatus: TaskPlanReview,
		Detail: "plan ready for review",
	})
	require.NoError(t, err)
	require.True(t, ok)
	require.True(t, IsTerminal(TaskPlanReview), "a plan-review task must never be re-leased or swept")
}
