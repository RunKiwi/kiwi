package planner

import (
	"context"
	"testing"

	"github.com/ibreakthecloud/kiwi/pkg/store"

	"github.com/ibreakthecloud/kiwi/pkg/agent"
)

// One task, one worker, one branch, one PR. The DAG existed to divide work into
// file-sized pieces for a single-turn Actor; an Implementer that can grep needs
// no such division.
func TestSessionPlanEmitsExactlyOneWorker(t *testing.T) {
	plan, err := NewSessionPlanner().Plan(context.Background(), PlanRequest{
		Task:           "add retries to the fetch path",
		Model:          "claude-sonnet-5",
		ArchitectModel: "claude-opus-4-8",
		Mode:           agent.ModeSession,
	})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if len(plan.Workers) != 1 {
		t.Fatalf("expected one worker, got %d", len(plan.Workers))
	}
	w := plan.Workers[0]
	if w.Mode != agent.ModeSession {
		t.Errorf("mode = %q", w.Mode)
	}
	if w.Model != "claude-sonnet-5" || w.ArchitectModel != "claude-opus-4-8" {
		t.Errorf("models not carried: %+v", w)
	}
	if len(w.DependsOn) != 0 {
		t.Errorf("a single worker cannot depend on anything: %v", w.DependsOn)
	}
}

// A file hint the Control Plane cannot check against the repository is exactly
// what session mode exists to stop relying on. Carrying one through would
// reintroduce it under another name.
func TestSessionPlanDropsFileHints(t *testing.T) {
	plan, err := NewSessionPlanner().Plan(context.Background(), PlanRequest{
		Task:  "add retries",
		Model: "claude-sonnet-5",
		File:  "src/fetch.go",
		Files: []string{"src/fetch.go", "src/retry.go"},
	})
	if err != nil {
		t.Fatal(err)
	}
	w := plan.Workers[0]
	if w.File != "" || len(w.Files) != 0 {
		t.Errorf("file hints must not reach a session worker: file=%q files=%v", w.File, w.Files)
	}
}

// The one-worker plan must still satisfy the validator every plan goes through,
// or session mode would be rejected at submit by a rule written for DAGs.
func TestSessionPlanPassesValidation(t *testing.T) {
	plan, err := NewSessionPlanner().Plan(context.Background(), PlanRequest{Task: "x", Model: "m"})
	if err != nil {
		t.Fatal(err)
	}
	if err := plan.Validate(defaultMaxWorkersPerJob); err != nil {
		t.Fatalf("a session plan must validate: %v", err)
	}
	if err := plan.Validate(1); err != nil {
		t.Fatalf("one worker must fit a limit of one: %v", err)
	}
}

func TestSessionPlanRequiresATask(t *testing.T) {
	if _, err := NewSessionPlanner().Plan(context.Background(), PlanRequest{Model: "m"}); err == nil {
		t.Fatal("expected an error for an empty task")
	}
}

// Learnings are resolved on the Control Plane, which owns the vector index, and
// consumed in the daemon, which now does the planning. Reducing them to text is
// deliberate: a learning is context, not a record to go and look up.
func TestLearningSummariesCarryTaskAndSummaryOnly(t *testing.T) {
	got := learningSummaries(sampleLearnings())
	if len(got) != 3 {
		t.Fatalf("expected three lines, got %d: %v", len(got), got)
	}
	if got[0] != "fix the parser — replaced the tokenizer" {
		t.Errorf("line 0 = %q", got[0])
	}
	if got[1] != "summary only" {
		t.Errorf("line 1 = %q", got[1])
	}
	if got[2] != "task only" {
		t.Errorf("line 2 = %q", got[2])
	}
}

func sampleLearnings() []store.JobLearning {
	return []store.JobLearning{
		{Task: "fix the parser", Summary: "replaced the tokenizer"},
		{Summary: "summary only"},
		{Task: "task only"},
		{},
	}
}
