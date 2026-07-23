package planner_test

import (
	"context"
	"strings"
	"testing"

	"github.com/ibreakthecloud/kiwi/pkg/planner"
	"github.com/ibreakthecloud/kiwi/pkg/store"
)

func learning(task, summary string) store.JobLearning {
	return store.JobLearning{Task: task, Repo: "owner/repo", Summary: summary}
}

type captureCompleter struct {
	lastUser string
}

func (c *captureCompleter) Complete(ctx context.Context, system, user string) (string, error) {
	c.lastUser = user
	return `{"summary":"ok", "workers": [{"id":"w1","task":"t1","file":"f1","model":"m1","test_cmd":"test"}]}`, nil
}

func TestLLMPlannerInjectsLearnings(t *testing.T) {
	comp := &captureCompleter{}
	p := planner.NewLLMPlanner(comp)

	outcome := "succeeded"
	pr := "https://github.com/owner/repo/pull/1"

	req := planner.PlanRequest{
		Task:       "do something",
		RepoURL:    "https://github.com/owner/repo",
		Ref:        "main",
		File:       "main.go",
		TestCmd:    "go test",
		MaxWorkers: 2,
		ResolvedLearnings: []store.JobLearning{
			{
				Task:    "past task 1",
				Repo:    "owner/repo",
				Outcome: &outcome,
				PRURL:   &pr,
				Summary: "did a thing",
			},
		},
	}

	_, err := p.Plan(context.Background(), req)
	if err != nil {
		t.Fatalf("Plan failed: %v", err)
	}

	if !strings.Contains(comp.lastUser, "# Prior related work (for context)") {
		t.Errorf("Expected context block header, got user prompt:\n%s", comp.lastUser)
	}

	expectedLine := "past task 1 • owner/repo • succeeded • did a thing • https://github.com/owner/repo/pull/1"
	if !strings.Contains(comp.lastUser, expectedLine) {
		t.Errorf("Expected learning line %q, got user prompt:\n%s", expectedLine, comp.lastUser)
	}
}

// No resolved learnings => no context block at all (the "off" path end-to-end).
func TestLLMPlannerNoLearningsNoBlock(t *testing.T) {
	comp := &captureCompleter{}
	p := planner.NewLLMPlanner(comp)
	if _, err := p.Plan(context.Background(), planner.PlanRequest{Task: "t", RepoURL: "https://github.com/o/r"}); err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if strings.Contains(comp.lastUser, "# Prior related work") {
		t.Errorf("did not expect a context block with no learnings:\n%s", comp.lastUser)
	}
}

// The injected block is bounded: at most three learnings, and an oversized
// summary is truncated rather than passed through whole.
func TestLLMPlannerInjectionIsCapped(t *testing.T) {
	comp := &captureCompleter{}
	p := planner.NewLLMPlanner(comp)

	huge := strings.Repeat("x", 3000)
	req := planner.PlanRequest{
		Task:    "new",
		RepoURL: "https://github.com/o/r",
		ResolvedLearnings: []store.JobLearning{
			learning("past task 1", huge),
			learning("past task 2", "s2"),
			learning("past task 3", "s3"),
			learning("past task 4", "s4"),
			learning("past task 5", "s5"),
		},
	}
	if _, err := p.Plan(context.Background(), req); err != nil {
		t.Fatalf("Plan: %v", err)
	}

	// Top-3 only: the 4th and 5th learnings must not appear.
	for _, absent := range []string{"past task 4", "past task 5"} {
		if strings.Contains(comp.lastUser, absent) {
			t.Errorf("expected %q to be dropped by the top-3 cap", absent)
		}
	}
	// The 3000-char summary is truncated to ~2000 chars — the full run never appears.
	if strings.Contains(comp.lastUser, huge) {
		t.Errorf("oversized summary was not truncated")
	}
	if !strings.Contains(comp.lastUser, strings.Repeat("x", 2000)) {
		t.Errorf("expected the truncated summary prefix to remain")
	}
}
