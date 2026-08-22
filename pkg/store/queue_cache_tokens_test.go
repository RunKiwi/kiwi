package store

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestQueuedTaskCacheTokenColumnsRoundTrip(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	task := &QueuedTask{
		ID: "t-cache-1", OrgID: "org-1", Status: TaskQueued, Spec: map[string]interface{}{},
	}
	require.NoError(t, s.EnqueueTask(ctx, task))
	leased, err := s.LeaseNextTask(ctx, "org-1", "daemon-1", "", time.Minute)
	require.NoError(t, err)

	ok, err := s.CompleteTask(ctx, TaskCompletion{
		TaskID: "t-cache-1", LeaseID: *leased.LeaseID, FinalStatus: TaskSucceeded,
		TokensIn: 1000, TokensOut: 200,
		CachedPromptTokens: 800, RawPromptTokens: 200,
	})
	require.NoError(t, err)
	require.True(t, ok)

	got, err := s.GetQueuedTask(ctx, "t-cache-1")
	require.NoError(t, err)
	require.Equal(t, int64(800), got.CachedPromptTokens)
	require.Equal(t, int64(200), got.RawPromptTokens)
}
