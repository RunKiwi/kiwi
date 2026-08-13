package session

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ibreakthecloud/kiwi/pkg/provider"
)

// toolCapableProvider satisfies both provider.Provider (Complete/GetCodeEdit)
// and provider.ToolRunner (StartConversation) — the same shape a real
// Anthropic/OpenAI/Gemini provider has. LLMArchitect type-asserts for
// ToolRunner rather than requiring it, so tests need a double that can
// present either interface depending on what's being exercised.
type toolCapableProvider struct {
	*provider.MockProvider
	*provider.MockToolRunner
}

func newToolCapableProvider(script func(n int, text string, results []provider.ToolResult) (provider.Turn, error)) *toolCapableProvider {
	return &toolCapableProvider{
		MockProvider:   provider.NewMockProvider(),
		MockToolRunner: &provider.MockToolRunner{Script: script},
	}
}

// The point of giving the Architect tools is that it reads real content
// instead of reasoning from a filename listing — this proves a tool result
// actually reaches the script that produces the final spec, not just that
// the plumbing type-checks.
func TestArchitectExploresBeforeAnswering(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "store.go"), []byte("package store\n\nfunc UsedElsewhere() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	tp := newToolCapableProvider(func(n int, text string, results []provider.ToolResult) (provider.Turn, error) {
		if n == 1 {
			return provider.Turn{Calls: []provider.ToolCall{
				provider.MockCall("r1", ToolReadFile, map[string]string{"path": "store.go"}),
			}}, nil
		}
		seen := ""
		if len(results) > 0 {
			seen = results[0].Content
		}
		objective := "did not see the file"
		if strings.Contains(seen, "UsedElsewhere") {
			objective = "found UsedElsewhere"
		}
		return provider.Turn{Text: `{"verdict":"proceed","objective":"` + objective + `","acceptance_criteria":["x"]}`}, nil
	})

	arch := &LLMArchitect{Provider: tp, Tools: NewArchitectTools(root)}
	spec, err := arch.Plan(context.Background(), PlanInput{Task: "task", RepoMap: []string{"store.go"}, MaxRoundsBudget: 3})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if spec.Objective != "found UsedElsewhere" {
		t.Errorf("objective = %q — the read_file result never reached the model's response", spec.Objective)
	}
	if len(tp.MockToolRunner.Started) != 1 {
		t.Fatalf("expected one conversation started, got %d", len(tp.MockToolRunner.Started))
	}
	if usage := arch.Usage(); usage.InputTokens == 0 {
		t.Error("exploration usage was never accumulated into Architect.Usage()")
	}
}

// A provider that cannot hold a tool conversation must not turn a valid
// submit into a failure — the same "decline, don't reject" stance
// architectModelFor takes for the model default.
func TestArchitectFallsBackToCompleteWithoutToolSupport(t *testing.T) {
	mp := provider.NewMockProvider()
	mp.CompleteFunc = func(system, user string) (string, error) {
		return `{"verdict":"proceed","objective":"done via complete","acceptance_criteria":["x"]}`, nil
	}
	arch := &LLMArchitect{Provider: mp, Tools: NewArchitectTools(t.TempDir())}
	spec, err := arch.Plan(context.Background(), PlanInput{Task: "task", MaxRoundsBudget: 3})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if spec.Objective != "done via complete" {
		t.Errorf("objective = %q, want the Complete() fallback to have run", spec.Objective)
	}
}

// Nil Tools is the same as today: no exploration, regardless of what the
// provider supports.
func TestArchitectWithNoToolsUsesCompleteEvenIfProviderCanExplore(t *testing.T) {
	calls := 0
	tp := newToolCapableProvider(func(n int, text string, results []provider.ToolResult) (provider.Turn, error) {
		calls++
		return provider.Turn{Text: `{"verdict":"proceed","objective":"x","acceptance_criteria":["x"]}`}, nil
	})
	tp.MockProvider.CompleteFunc = func(system, user string) (string, error) {
		return `{"verdict":"proceed","objective":"via complete","acceptance_criteria":["x"]}`, nil
	}
	arch := &LLMArchitect{Provider: tp}
	if _, err := arch.Plan(context.Background(), PlanInput{Task: "task", MaxRoundsBudget: 3}); err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if calls != 0 {
		t.Errorf("StartConversation-based script ran %d times with Tools nil; Complete should have been used instead", calls)
	}
	if len(tp.MockToolRunner.Started) != 0 {
		t.Error("a conversation was started despite Tools being nil")
	}
}

// MaxToolCalls bounds exploration independently of the dollar budget: an
// Architect that could read without limit would compete with the
// Implementer for the same $/task ceiling before any code gets written.
func TestArchitectExplorationCapForcesAnAnswer(t *testing.T) {
	scriptCalls := 0
	tp := newToolCapableProvider(func(n int, text string, results []provider.ToolResult) (provider.Turn, error) {
		scriptCalls++
		if strings.Contains(text, "exploration budget") {
			return provider.Turn{Text: `{"verdict":"proceed","objective":"forced","acceptance_criteria":["x"]}`}, nil
		}
		return provider.Turn{Calls: []provider.ToolCall{
			provider.MockCall("r", ToolListFiles, map[string]string{}),
		}}, nil
	})

	arch := &LLMArchitect{Provider: tp, Tools: NewArchitectTools(t.TempDir()), MaxToolCalls: 2}
	spec, err := arch.Plan(context.Background(), PlanInput{Task: "task", MaxRoundsBudget: 3})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if spec.Objective != "forced" {
		t.Errorf("objective = %q, expected the forced-answer turn to win", spec.Objective)
	}
	// 2 turns that returned tool calls (the cap) + 1 forced-answer turn.
	if scriptCalls != 3 {
		t.Errorf("script ran %d times, want 3 (2 exploring + 1 forced answer)", scriptCalls)
	}
}

// The Architect must never edit the repository — that's the Implementer's
// job. Call refuses a write even if a model somehow names a tool it was
// never offered, rather than relying on Defs() alone to keep it honest.
func TestArchitectToolsRejectsWrite(t *testing.T) {
	at := NewArchitectTools(t.TempDir())
	res, err := at.Call(context.Background(), provider.MockCall("c", ToolWriteFile, map[string]string{"path": "x.go", "content": "package x"}))
	if err != nil {
		t.Fatalf("Call returned a fatal error rather than a model-visible refusal: %v", err)
	}
	if !res.IsError {
		t.Fatal("write_file should have been refused")
	}
}

func TestArchitectToolsDefsAreReadOnly(t *testing.T) {
	at := NewArchitectTools(t.TempDir())
	forbidden := map[string]bool{ToolWriteFile: true, ToolEditFile: true, ToolRun: true, ToolInstall: true, ToolFinish: true}
	for _, d := range at.Defs() {
		if forbidden[d.Name] {
			t.Errorf("Defs() offered a write/run/finish tool: %s", d.Name)
		}
	}
	if len(at.Defs()) != 3 {
		t.Errorf("expected exactly list_files, read_file, grep — got %d defs", len(at.Defs()))
	}
}
