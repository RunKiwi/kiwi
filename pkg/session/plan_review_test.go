package session

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRunStopsAfterPlanningWhenApprovalRequired(t *testing.T) {
	architect := &fakeArchitect{
		plan: Spec{Verdict: VerdictProceed, Objective: "add a test"},
	}
	tools, _ := newTools(t)
	runner := &Runner{
		Architect:   architect,
		Implementer: &fakeToolRunner{}, // must NOT be called
		Tools:       tools,
		Workspace:   &fakeWorkspace{},
		Verify:      func(ctx context.Context) (string, bool, error) { return "", true, nil },
		Config:      Config{MaxRounds: 3},
	}

	res, err := runner.Run(context.Background(), Task{
		ID:                   "t1",
		Description:          "add a test",
		TestCmd:              "true",
		RequiresPlanApproval: true,
	})

	require.NoError(t, err)
	require.True(t, res.PlanPendingReview)
	require.Equal(t, "add a test", res.Spec.Objective)
	require.Equal(t, 0, architect.reviewCalls, "review must not run before approval")
	require.Equal(t, 0, runner.Implementer.(*fakeToolRunner).callCount, "the implementer must not run before approval")
}
