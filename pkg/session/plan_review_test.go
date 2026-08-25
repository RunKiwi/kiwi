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

func TestRunRePlansWhenRevisionFeedbackGiven(t *testing.T) {
	arch := &fakeArchitect{plan: Spec{Verdict: VerdictProceed, Objective: "first attempt"}}
	ws := &fakeWorkspace{tree: []string{"a.go"}, head: "base"}
	store := &memStore{}
	runner := durableRunner(t, arch, ws, store)

	task := Task{
		Description:          "add a test",
		TestCmd:              "true",
		RequiresPlanApproval: true,
	}
	res, err := runner.Run(context.Background(), task)
	require.NoError(t, err)
	require.True(t, res.PlanPendingReview)
	require.Equal(t, 1, arch.plannedCalls)

	// A human rejected the plan with feedback. The continuation reuses the
	// same session (same checkpoint) but carries the feedback.
	arch.plan = Spec{Verdict: VerdictProceed, Objective: "revised attempt"}
	task.RevisionFeedback = "use CockroachDB leases instead"
	res2, err := runner.Run(context.Background(), task)

	require.NoError(t, err)
	require.True(t, res2.PlanPendingReview, "a revised plan must stop for review again, not run the Implementer")
	require.Equal(t, "revised attempt", res2.Spec.Objective)
	require.Equal(t, 2, arch.plannedCalls, "revision must re-plan, not resume into the round loop")
	require.Equal(t, 0, arch.reviewCalls, "no round ever ran, so there is nothing to review")
	require.Len(t, arch.planSeen, 2)
	require.Contains(t, arch.planSeen[1].Task, "add a test", "the original task must still be present")
	require.Contains(t, arch.planSeen[1].Task, "use CockroachDB leases instead")

	// The human approved the revised plan. Approving must copy the parent
	// spec without its feedback (see ee/orchestrator/plan_api.go's
	// handleApproveJobPlan) — modeled here by clearing RevisionFeedback on
	// the same task before the next Run, exactly as that fresh spec would
	// arrive. This is the regression guard for the bug where a stale
	// "revision_feedback" on an approved continuation made the session
	// re-plan forever instead of ever reaching the Implementer.
	task.RevisionFeedback = ""
	res3, err := runner.Run(context.Background(), task)
	require.NoError(t, err)
	require.True(t, res3.Success)
	require.Equal(t, 2, arch.plannedCalls, "approval must resume into the round loop, not re-plan a third time")
	require.Equal(t, 1, arch.reviewCalls, "the round that finally ran must be reviewed")
}
