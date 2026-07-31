package planner

import (
	"context"
	"strings"
	"testing"
)

// The test command is the definition of done. The submitter's command wins over
// anything a planner suggests, for the same reason the worker model does: the
// planner never sees the repo, so its value is a guess.
func TestSubmittedTestCommandBeatsThePlanners(t *testing.T) {
	got := workerTestCmd(
		PlannedWorker{TestCmd: "npm test"},
		PlanRequest{TestCmd: "npm run build"},
	)
	if got != "npm run build" {
		t.Errorf("got %q, want the submitter's %q", got, "npm run build")
	}
}

// With nobody supplying one, the value must stay empty so the daemon can infer
// it from the repository's own marker files. A planner guess here would
// suppress that inference — which is how "npm test" reached a project with no
// test script, where npm *errors* rather than failing, and the loop spent its
// whole budget trying to satisfy a script that does not exist.
func TestNoTestCommandStaysEmptyForRepoInference(t *testing.T) {
	if got := workerTestCmd(PlannedWorker{}, PlanRequest{}); got != "" {
		t.Errorf("got %q, want \"\" so inferTestCmd can decide", got)
	}
}

// The LLM planner must not emit a test command at all, so there is nothing to
// suppress repo-based inference with.
func TestPlannerIsNotAskedToChooseATestCommand(t *testing.T) {
	if strings.Contains(plannerSystem, "test_cmd") {
		t.Errorf("the planner schema still asks for test_cmd:\n%s", plannerSystem)
	}
	if !strings.Contains(plannerSystem, "test commands") {
		t.Error("the planner should be told the runtime assigns test commands")
	}
}

// Whatever the planner returns for test_cmd is ignored end to end, not merely
// deprioritised in the struct.
func TestPlannerSuppliedTestCommandDoesNotReachTheWorker(t *testing.T) {
	p := NewLLMPlanner(&testCmdFake{})

	plan, err := p.Plan(context.Background(), PlanRequest{Task: "add a footer"})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if got := workerTestCmd(plan.Workers[0], PlanRequest{}); got != "" {
		t.Errorf("planner test command leaked through as %q", got)
	}
}

type testCmdFake struct{}

func (testCmdFake) Complete(ctx context.Context, system, user string) (string, error) {
	// A model that ignores the instruction and names one anyway.
	return `{"summary":"s","workers":[{"id":"w1","task":"t","file":"f","test_cmd":"npm test"}]}`, nil
}
