package session

import (
	"context"
	"strings"
	"testing"

	"github.com/ibreakthecloud/kiwi/pkg/provider"
)

// The dashboard's "running now" indicator is set from OnActivity, and until it
// existed nothing set it except sandbox commands — the test command, the
// dependency install, and the Implementer's `run` tool.
//
// That did not merely leave a gap. The indicator holds its last value, so while
// the Architect spent minutes planning, the dashboard went on showing the test
// command that had already finished. A user watching a run was told the wrong
// thing, confidently, for the longest stretch of it.
func TestActivityReportsTheModelCallsNotJustTheSandboxOnes(t *testing.T) {
	var seen []string
	r := newActivityRunner(t, &seen)

	if _, err := r.Run(context.Background(), Task{
		Description: "add retries",
		TestCmd:     "go test ./...",
	}); err != nil {
		t.Fatalf("Run: %v", err)
	}

	joined := strings.Join(seen, " | ")
	for _, want := range []string{"architect", "implementer"} {
		if !containsWord(seen, want) {
			t.Errorf("no activity mentioned %q; got: %s", want, joined)
		}
	}
}

// The planning phase is the one this was built for: a single model call, no
// sandbox command, minutes long on a frontier model.
func TestActivityAnnouncesPlanningBeforeItStarts(t *testing.T) {
	var seen []string
	r := newActivityRunner(t, &seen)

	// The Architect's Plan blocks until it reports; the activity naming it has
	// to be set before the call, not after, or it describes the past.
	r.Architect = &scriptedArchitect{
		specs: []Spec{
			{Verdict: VerdictApprove, Summary: "nothing to do"},
		},
		onPlan: func() {
			if len(seen) == 0 || !strings.Contains(strings.ToLower(seen[len(seen)-1]), "architect") {
				t.Errorf("planning started with activity %v; expected the Architect to be named first", seen)
			}
		},
	}

	if _, err := r.Run(context.Background(), Task{Description: "x", TestCmd: "true"}); err != nil {
		t.Fatalf("Run: %v", err)
	}
}

// A nil hook must be as safe as any other unset Config field.
func TestActivityHookIsOptional(t *testing.T) {
	r := newActivityRunner(t, nil)
	r.Config.OnActivity = nil
	if _, err := r.Run(context.Background(), Task{Description: "x", TestCmd: "true"}); err != nil {
		t.Fatalf("Run with no OnActivity: %v", err)
	}
}

func containsWord(all []string, want string) bool {
	for _, s := range all {
		if strings.Contains(strings.ToLower(s), want) {
			return true
		}
	}
	return false
}

// scriptedArchitect returns specs in order, and can assert on the state of the
// world at the moment Plan is entered.
type scriptedArchitect struct {
	specs  []Spec
	n      int
	onPlan func()
}

func (a *scriptedArchitect) Plan(context.Context, PlanInput) (Spec, error) {
	if a.onPlan != nil {
		a.onPlan()
	}
	return a.next(), nil
}

func (a *scriptedArchitect) Review(context.Context, ReviewInput) (Spec, error) { return a.next(), nil }
func (a *scriptedArchitect) Usage() provider.ToolUsage                         { return provider.ToolUsage{} }

func (a *scriptedArchitect) next() Spec {
	s := a.specs[len(a.specs)-1]
	if a.n < len(a.specs) {
		s = a.specs[a.n]
	}
	a.n++
	return s
}

func newActivityRunner(t *testing.T, seen *[]string) *Runner {
	t.Helper()
	impl := &provider.MockToolRunner{}
	impl.Script = func(n int, text string, results []provider.ToolResult) (provider.Turn, error) {
		if n == 1 {
			return provider.Turn{Calls: []provider.ToolCall{
				provider.MockCall("c1", ToolWriteFile, map[string]string{"path": "a.go", "content": "x"}),
			}}, nil
		}
		return provider.Turn{Calls: []provider.ToolCall{
			provider.MockCall("c2", ToolFinish, map[string]string{"note": "done"}),
		}}, nil
	}

	arch := &scriptedArchitect{specs: []Spec{
		{Verdict: VerdictProceed, Objective: "write a.go"},
		{Verdict: VerdictApprove, Summary: "done"},
	}}
	ws := &fakeWorkspace{tree: []string{"main.go"}, diff: "+x", head: "base"}
	r, _ := newRunner(t, arch, impl, ws, passing("ok"))
	r.Config.MaxRounds = 2
	r.Config.OnActivity = func(a string) {
		if seen != nil {
			*seen = append(*seen, a)
		}
	}
	return r
}
