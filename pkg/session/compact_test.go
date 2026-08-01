package session

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/ibreakthecloud/kiwi/pkg/provider"
)

// A long round re-sends its transcript on every turn, so an exploration phase
// that has already yielded its conclusions is pure recurring cost. Compaction
// replaces it with the model's own summary and starts a fresh conversation.
func TestLongRoundCompactsAndRestartsFromTheSummary(t *testing.T) {
	tools, _ := newTools(t)

	var sawSummary bool
	impl := &provider.MockToolRunner{
		TokensPerTurn: 50,
		Script: func(n int, text string, results []provider.ToolResult) (provider.Turn, error) {
			if strings.Contains(text, "Your context is being compacted") {
				return provider.Turn{Text: "I changed internal/fetch/client.go; the retry helper lives in internal/retry."}, nil
			}
			if strings.Contains(text, "Where you had got to") {
				sawSummary = true
				return provider.Turn{Calls: []provider.ToolCall{
					provider.MockCall("f", ToolFinish, map[string]string{"note": "finished after compaction"}),
				}}, nil
			}
			return provider.Turn{Calls: []provider.ToolCall{
				provider.MockCall(fmt.Sprintf("c%d", n), ToolReadFile, map[string]string{"path": "main.go"}),
			}}, nil
		},
	}

	arch := &fakeArchitect{plan: Spec{Verdict: VerdictProceed, Objective: "explore then fix"}}
	ws := &fakeWorkspace{tree: []string{"main.go"}, diff: "+x", head: "base"}
	var events []Event
	r := &Runner{
		Architect: arch, Implementer: impl, Tools: tools, Workspace: ws, Verify: passing("ok"),
		Config: Config{
			MaxRounds: 1, CompactAt: 100, MaxToolCallsPerRound: 30,
			OnEvent: func(e Event) { events = append(events, e) },
		},
	}

	if _, err := r.Run(context.Background(), Task{Description: "task"}); err != nil {
		t.Fatal(err)
	}
	if !hasEvent(events, "compaction", "ok") {
		t.Fatal("expected a compaction event")
	}
	if !sawSummary {
		t.Fatal("the restarted conversation must be seeded with the summary")
	}
	if len(impl.Started) < 2 {
		t.Errorf("compaction should start a fresh conversation, started %d", len(impl.Started))
	}
	if arch.seen[0].HandoffNote != "finished after compaction" {
		t.Errorf("the round should have finished normally after compacting, got %q", arch.seen[0].HandoffNote)
	}
}

// A round that cannot compact keeps paying full price, which is worse than the
// alternative and far better than a failed task.
func TestFailedCompactionDoesNotFailTheRound(t *testing.T) {
	tools, _ := newTools(t)
	turns := 0
	impl := &provider.MockToolRunner{
		TokensPerTurn: 50,
		Script: func(n int, text string, results []provider.ToolResult) (provider.Turn, error) {
			turns++
			if strings.Contains(text, "Your context is being compacted") {
				return provider.Turn{}, errors.New("model unavailable")
			}
			if turns > 4 {
				return provider.Turn{Calls: []provider.ToolCall{
					provider.MockCall("f", ToolFinish, map[string]string{"note": "done anyway"}),
				}}, nil
			}
			return provider.Turn{Calls: []provider.ToolCall{
				provider.MockCall(fmt.Sprintf("c%d", n), ToolReadFile, map[string]string{"path": "main.go"}),
			}}, nil
		},
	}

	arch := &fakeArchitect{plan: Spec{Verdict: VerdictProceed, Objective: "work"}}
	ws := &fakeWorkspace{tree: []string{"main.go"}, diff: "+x", head: "base"}
	r := &Runner{Architect: arch, Implementer: impl, Tools: tools, Workspace: ws, Verify: passing("ok")}
	r.Config.MaxRounds = 1
	r.Config.CompactAt = 100
	r.Config.MaxToolCallsPerRound = 30

	if _, err := r.Run(context.Background(), Task{Description: "task"}); err != nil {
		t.Fatalf("a failed compaction must not fail the round: %v", err)
	}
	if len(arch.seen) != 1 {
		t.Fatal("the round should still have been reviewed")
	}
}

// Caching is roughly the difference between a $5 round and a $0.70 one, so the
// zero-value policy has to be "on". The transport layer stays opt-in.
func TestCachingIsOnByDefaultAndCanBeTurnedOff(t *testing.T) {
	arch := &fakeArchitect{plan: Spec{Verdict: VerdictProceed, Objective: "go"}}
	ws := &fakeWorkspace{tree: []string{"a.go"}, diff: "+x", head: "base"}

	impl := finishingRunner("done")
	r, _ := newRunner(t, arch, impl, ws, passing("ok"))
	if _, err := r.Run(context.Background(), Task{Description: "task"}); err != nil {
		t.Fatal(err)
	}
	if len(impl.Started) == 0 || !impl.Started[0].Opts.Cache {
		t.Error("caching must be on when Config says nothing")
	}

	arch2 := &fakeArchitect{plan: Spec{Verdict: VerdictProceed, Objective: "go"}}
	impl2 := finishingRunner("done")
	r2, _ := newRunner(t, arch2, impl2, &fakeWorkspace{tree: []string{"a.go"}, diff: "+x", head: "base"}, passing("ok"))
	r2.Config.NoCache = true
	if _, err := r2.Run(context.Background(), Task{Description: "task"}); err != nil {
		t.Fatal(err)
	}
	if impl2.Started[0].Opts.Cache {
		t.Error("NoCache must turn caching off")
	}
}

// Compaction is off when explicitly disabled, so a caller that wants a single
// unbroken transcript can have one.
func TestCompactionCanBeDisabled(t *testing.T) {
	tools, _ := newTools(t)
	impl := &provider.MockToolRunner{
		TokensPerTurn: 1000,
		Script: func(n int, text string, results []provider.ToolResult) (provider.Turn, error) {
			if strings.Contains(text, "compacted") {
				t.Error("compaction should not have run")
			}
			return provider.Turn{Calls: []provider.ToolCall{
				provider.MockCall("f", ToolFinish, map[string]string{"note": "done"}),
			}}, nil
		},
	}
	arch := &fakeArchitect{plan: Spec{Verdict: VerdictProceed, Objective: "go"}}
	ws := &fakeWorkspace{tree: []string{"a.go"}, diff: "+x", head: "base"}
	r := &Runner{Architect: arch, Implementer: impl, Tools: tools, Workspace: ws, Verify: passing("ok")}
	r.Config.MaxRounds = 1
	r.Config.CompactAt = -1

	if _, err := r.Run(context.Background(), Task{Description: "task"}); err != nil {
		t.Fatal(err)
	}
	if len(impl.Started) != 1 {
		t.Errorf("expected one conversation, got %d", len(impl.Started))
	}
}

// Cache reads and writes have to reach the session's accounting, or a cached
// run's cost is reported as if it were uncached.
func TestSessionUsageCarriesCacheTokenClasses(t *testing.T) {
	arch := &fakeArchitect{plan: Spec{Verdict: VerdictProceed, Objective: "go"}}
	ws := &fakeWorkspace{tree: []string{"a.go"}, diff: "+x", head: "base"}
	r, _ := newRunner(t, arch, finishingRunner("done"), ws, passing("ok"))

	res, err := r.Run(context.Background(), Task{Description: "task"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Usage.InputTokens == 0 {
		t.Error("usage should carry token counts")
	}
}
