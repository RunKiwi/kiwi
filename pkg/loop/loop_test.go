package loop

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ibreakthecloud/kiwi/pkg/provider"
)

// scriptedProvider returns a queued sequence of edits, one per Actor call, so a
// test can script the agent's behavior deterministically without a network.
type scriptedProvider struct {
	edits []string
	calls int
}

func (p *scriptedProvider) GetCodeEdit(ctx context.Context, task, fileName, code, buildOutput string) (string, error) {
	e := p.edits[p.calls%len(p.edits)]
	p.calls++
	return e, nil
}

// passWhenContains makes runTest pass once the file contains `marker`. On
// failure it echoes the current file content in the output — as real
// compiler/test output does — so distinct edits produce distinct failures and
// the stall guard only trips on a genuinely repeated state.
func passWhenContains(path, marker string) TestFunc {
	return func(ctx context.Context) (string, bool, error) {
		b, err := os.ReadFile(path)
		if err != nil {
			return "", false, err
		}
		if strings.Contains(string(b), marker) {
			return "ok", true, nil
		}
		return "FAIL: want " + marker + ", have: " + string(b), false, nil
	}
}

func writeTemp(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "target.txt")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("write temp: %v", err)
	}
	return p
}

func TestLoop_InitialTestPasses_NoEdits(t *testing.T) {
	path := writeTemp(t, "already good")
	prov := &scriptedProvider{edits: []string{"SHOULD NOT BE CALLED"}}
	r := &Runner{Provider: prov}

	res, err := r.Run(context.Background(), Task{Description: "noop", FilePath: path},
		passWhenContains(path, "already good"))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.Success || res.Steps != 0 {
		t.Errorf("got success=%v steps=%d, want true/0", res.Success, res.Steps)
	}
	if prov.calls != 0 {
		t.Errorf("Actor was called %d times; a passing initial test must skip editing", prov.calls)
	}
}

func TestLoop_ActorFixesOnFirstTry(t *testing.T) {
	path := writeTemp(t, "broken")
	prov := &scriptedProvider{edits: []string{"now FIXED"}}
	r := &Runner{Provider: prov}

	res, err := r.Run(context.Background(), Task{Description: "fix it", FilePath: path},
		passWhenContains(path, "FIXED"))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.Success || res.Steps != 1 {
		t.Errorf("got success=%v steps=%d, want true/1", res.Success, res.Steps)
	}
	if b, _ := os.ReadFile(path); string(b) != "now FIXED" {
		t.Errorf("file = %q, want the applied edit", b)
	}
}

func TestLoop_IteratesUntilPass(t *testing.T) {
	path := writeTemp(t, "v0")
	// Two wrong tries, then the fix.
	prov := &scriptedProvider{edits: []string{"v1 nope", "v2 nope", "v3 FIXED"}}
	r := &Runner{Provider: prov}

	res, err := r.Run(context.Background(), Task{Description: "fix it", FilePath: path},
		passWhenContains(path, "FIXED"))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.Success || res.Steps != 3 {
		t.Errorf("got success=%v steps=%d, want true/3", res.Success, res.Steps)
	}
}

func TestLoop_CriticRejectionGatesTheEdit(t *testing.T) {
	path := writeTemp(t, "broken")
	// The Actor always proposes the fix; the Critic rejects the first attempt,
	// so the fix is only applied on the second Actor turn.
	prov := &scriptedProvider{edits: []string{"FIXED but rejected first", "FIXED"}}
	critic := &scriptedCritic{approve: []bool{false, true}}
	r := &Runner{Provider: prov, Critic: critic}

	res, err := r.Run(context.Background(), Task{Description: "fix it", FilePath: path},
		passWhenContains(path, "FIXED"))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	// Step 1: proposed, rejected, NOT applied (test not run). Step 2: approved,
	// applied, test passes.
	if !res.Success || res.Steps != 2 {
		t.Errorf("got success=%v steps=%d, want true/2", res.Success, res.Steps)
	}
	if critic.calls != 2 {
		t.Errorf("critic called %d times, want 2", critic.calls)
	}
}

func TestLoop_HaltsOnStall(t *testing.T) {
	path := writeTemp(t, "broken")
	// The Actor keeps proposing the same non-fixing edit; the identical failing
	// output must trip the stall guard rather than loop forever.
	prov := &scriptedProvider{edits: []string{"still broken"}}
	r := &Runner{Provider: prov, Config: Config{MaxSteps: 20}}

	res, err := r.Run(context.Background(), Task{Description: "fix it", FilePath: path},
		passWhenContains(path, "FIXED"))
	if err == nil {
		t.Fatal("expected a stall error, got nil")
	}
	if res.Success {
		t.Error("stalled loop must not report success")
	}
	if res.Steps >= 20 {
		t.Errorf("stall guard should stop well before MaxSteps; stopped at %d", res.Steps)
	}
}

func TestLoop_HaltsAtMaxSteps(t *testing.T) {
	path := writeTemp(t, "v0")
	// Every edit is distinct (so the stall guard never trips) but none fixes it.
	prov := &scriptedProvider{edits: []string{"a", "b", "c", "d", "e", "f", "g", "h"}}
	r := &Runner{Provider: prov, Config: Config{MaxSteps: 4}}

	res, err := r.Run(context.Background(), Task{Description: "fix it", FilePath: path},
		passWhenContains(path, "FIXED"))
	if err == nil {
		t.Fatal("expected a max-steps error, got nil")
	}
	if res.Success || res.Steps != 4 {
		t.Errorf("got success=%v steps=%d, want false/4", res.Success, res.Steps)
	}
}

func TestLoop_NoProviderIsError(t *testing.T) {
	r := &Runner{}
	_, err := r.Run(context.Background(), Task{FilePath: "/nope"},
		func(ctx context.Context) (string, bool, error) { return "", false, nil })
	if err == nil {
		t.Fatal("expected error when no provider is configured")
	}
}

type scriptedCritic struct {
	approve []bool
	calls   int
}

func (c *scriptedCritic) ReviewEdit(ctx context.Context, task, fileName, oldC, newC, buildOutput string) (provider.Verdict, error) {
	v := provider.Verdict{Approved: c.approve[c.calls%len(c.approve)]}
	c.calls++
	return v, nil
}
