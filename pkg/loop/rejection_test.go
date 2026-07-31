package loop

import (
	"context"
	"strings"
	"testing"

	"github.com/ibreakthecloud/kiwi/pkg/provider"
)

// alwaysRejects stands in for a Critic that is right and cannot be satisfied —
// the real one rejected "Go code in a .rs file" three times because the Actor
// may change a file's contents but never its name.
type alwaysRejects struct {
	calls   int
	reasons string
}

func (c *alwaysRejects) ReviewEdit(ctx context.Context, task, fileName, oldContent, newContent, buildOutput string) (provider.Verdict, error) {
	c.calls++
	return provider.Verdict{Approved: false, Reasons: c.reasons}, nil
}

// A rejection applies nothing, so a run that cannot satisfy the Critic makes no
// progress while still spending an Actor call, the budget and the clock. The
// real one died on the task deadline at ten minutes.
func TestRun_HaltsWhenTheCriticKeepsRejecting(t *testing.T) {
	path := writeTemp(t, "start")
	prov := &scriptedProvider{edits: []string{"a", "b", "c", "d", "e", "f"}}
	critic := &alwaysRejects{reasons: "contains Go code, but has a .rs file extension"}

	r := &Runner{Provider: prov, Critic: critic, Config: Config{MaxSteps: 6}}
	res, err := r.Run(context.Background(), Task{Description: "add an example", FilePath: path},
		func(ctx context.Context) (string, bool, error) { return "FAIL", false, nil })

	if err == nil {
		t.Fatal("expected the loop to halt rather than exhaust its steps")
	}
	if res.Steps > rejectionHalt {
		t.Errorf("ran %d steps; should stop at %d consecutive rejections", res.Steps, rejectionHalt)
	}
	if critic.calls != rejectionHalt {
		t.Errorf("Critic called %d times, want %d", critic.calls, rejectionHalt)
	}
	// The reason is the actionable part — without it the user learns only that
	// something was rejected.
	if !strings.Contains(err.Error(), ".rs file extension") {
		t.Errorf("the halt should carry the Critic's last reason, got %v", err)
	}
}

// A rejection followed by an approval is ordinary iteration — the Critic's
// feedback landing is the mechanism working, not a stall — so the counter must
// reset rather than accumulate across a whole run.
func TestRun_RejectionCounterResetsOnApproval(t *testing.T) {
	path := writeTemp(t, "start")
	prov := &scriptedProvider{edits: []string{"bad", "good FIXED"}}
	critic := &alternatingCritic{}

	r := &Runner{Provider: prov, Critic: critic, Config: Config{MaxSteps: 6}}
	res, err := r.Run(context.Background(), Task{Description: "fix it", FilePath: path},
		passWhenContains(path, "FIXED"))

	if err != nil {
		t.Fatalf("a run that recovers from a rejection must not halt: %v", err)
	}
	if !res.Success {
		t.Errorf("expected success, got %+v", res)
	}
}

// alternatingCritic rejects the first proposal and approves the second, the
// shape of an Actor that responds to feedback.
type alternatingCritic struct{ calls int }

func (c *alternatingCritic) ReviewEdit(ctx context.Context, task, fileName, oldContent, newContent, buildOutput string) (provider.Verdict, error) {
	c.calls++
	if c.calls == 1 {
		return provider.Verdict{Approved: false, Reasons: "incomplete"}, nil
	}
	return provider.Verdict{Approved: true}, nil
}
