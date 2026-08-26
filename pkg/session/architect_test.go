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

// When the Architect has tools, a parse failure must not restart exploration
// from scratch for the retry — that hands a weak model a fresh tool budget
// and no memory of why the first attempt failed, which is how the same model
// produces the same unparsed prose twice. The retry goes through Complete
// instead, which is also the one call shape a provider can constrain to valid
// JSON syntax without that fighting with tool-calling (see openai.go/
// gemini.go's jsonMode on Complete).
func TestArchitectPlanRetryDoesNotReexploreWithTools(t *testing.T) {
	tp := newToolCapableProvider(func(n int, text string, results []provider.ToolResult) (provider.Turn, error) {
		return provider.Turn{Text: "the model rambled without ever producing JSON"}, nil
	})
	tp.MockProvider.CompleteFunc = func(system, user string) (string, error) {
		return `{"verdict":"proceed","objective":"recovered via retry","acceptance_criteria":["x"]}`, nil
	}

	arch := &LLMArchitect{Provider: tp, Tools: NewArchitectTools(t.TempDir())}
	spec, err := arch.Plan(context.Background(), PlanInput{Task: "task", MaxRoundsBudget: 3})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if spec.Objective != "recovered via retry" {
		t.Errorf("objective = %q, want the Complete() retry to have supplied the spec", spec.Objective)
	}
	if len(tp.MockToolRunner.Started) != 1 {
		t.Errorf("expected exactly one exploration conversation, got %d — the retry re-explored instead of calling Complete", len(tp.MockToolRunner.Started))
	}
}

// If the corrective retry also fails to parse, the error must describe the
// retry's own response, not silently repeat the first attempt's — otherwise a
// caller debugging "no JSON object" sees a stale preview from a response that
// isn't the one that actually decided the failure.
func TestArchitectPlanRetryFailureNamesTheSecondResponse(t *testing.T) {
	calls := 0
	mp := provider.NewMockProvider()
	mp.CompleteFunc = func(system, user string) (string, error) {
		calls++
		if calls == 1 {
			return "the model rambled without ever producing JSON, attempt one", nil
		}
		return "distinctive-second-attempt-text, still no braces", nil
	}
	arch := &LLMArchitect{Provider: mp, Tools: NewArchitectTools(t.TempDir())}
	_, err := arch.Plan(context.Background(), PlanInput{Task: "task", MaxRoundsBudget: 3})
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
	if calls != 2 {
		t.Fatalf("expected the retry to run, got %d complete() calls", calls)
	}
	if !strings.Contains(err.Error(), "distinctive-second-attempt-text") {
		t.Errorf("error should describe the retry's own response, got: %v", err)
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

// A weak or free-tier model occasionally answers in prose instead of JSON.
// completeSpec must give it one corrective turn before failing the round.
func TestPlanRetriesOnceOnUnparsableResponse(t *testing.T) {
	calls := 0
	mp := provider.NewMockProvider()
	mp.CompleteFunc = func(system, user string) (string, error) {
		calls++
		if calls == 1 {
			return "Sure, here is my plan: I will fix the bug.", nil
		}
		if !strings.Contains(user, "could not be used") {
			t.Errorf("retry prompt did not carry the corrective nudge: %q", user)
		}
		return `{"verdict":"proceed","objective":"fix it","acceptance_criteria":["x"]}`, nil
	}
	arch := &LLMArchitect{Provider: mp}

	spec, err := arch.Plan(context.Background(), PlanInput{Task: "task", MaxRoundsBudget: 3})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if calls != 2 {
		t.Fatalf("Complete called %d times, want 2 (initial + retry)", calls)
	}
	if spec.Objective != "fix it" {
		t.Errorf("objective = %q, want the retry's response to win", spec.Objective)
	}
}

// A second unparsable response must not be retried again — one corrective
// turn is the budget, not a loop — and the surfaced error is the original
// failure, not the retry's.
func TestPlanFailsAfterOneFailedRetry(t *testing.T) {
	calls := 0
	mp := provider.NewMockProvider()
	mp.CompleteFunc = func(system, user string) (string, error) {
		calls++
		return "still no JSON here", nil
	}
	arch := &LLMArchitect{Provider: mp}

	_, err := arch.Plan(context.Background(), PlanInput{Task: "task", MaxRoundsBudget: 3})
	if err == nil {
		t.Fatal("expected an error after both attempts failed to parse")
	}
	if calls != 2 {
		t.Fatalf("Complete called %d times, want 2 (initial + retry, then give up)", calls)
	}
}

// TestReviewAllowsEmptyDiffOnlyWhenBothInvestigationOnlyAndNoDiffExpected
// covers the actual safeguard: an empty-diff approval must survive Review
// only when the CALLER flagged the task InvestigationOnly and THIS round's
// own verdict set no_diff_expected. Either alone must still be downgraded.
func TestReviewAllowsEmptyDiffOnlyWhenBothInvestigationOnlyAndNoDiffExpected(t *testing.T) {
	approveNoDiffExpected := `{"verdict":"approve","summary":"Found the root cause: a nil check missing in auth.go.","no_diff_expected":true}`
	approveOrdinary := `{"verdict":"approve","summary":"Fixed it."}`

	cases := []struct {
		name              string
		investigationOnly bool
		response          string
		wantVerdict       string
	}{
		{"both set: approval survives", true, approveNoDiffExpected, VerdictApprove},
		{"InvestigationOnly false: still downgraded even with no_diff_expected", false, approveNoDiffExpected, VerdictRevise},
		{"no_diff_expected false: an ordinary empty-diff approval is still downgraded", true, approveOrdinary, VerdictRevise},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			mp := provider.NewMockProvider()
			mp.CompleteFunc = func(system, user string) (string, error) { return c.response, nil }
			arch := &LLMArchitect{Provider: mp}

			spec, err := arch.Review(context.Background(), ReviewInput{
				Task: "investigate the bug", Round: 1, Diff: "", VerifyPassed: true,
				InvestigationOnly: c.investigationOnly,
			})
			if err != nil {
				t.Fatalf("Review: %v", err)
			}
			if spec.Verdict != c.wantVerdict {
				t.Errorf("verdict = %q, want %q", spec.Verdict, c.wantVerdict)
			}
		})
	}
}

// A code-fixing task must still fail on an empty diff even when the
// Architect's own response carries no_diff_expected — the regression this
// whole safeguard exists to prevent (CLAUDE.md's "returned early when the
// suite already passed" incident).
func TestReviewNeverBypassesEmptyDiffForAnOrdinaryTask(t *testing.T) {
	mp := provider.NewMockProvider()
	mp.CompleteFunc = func(system, user string) (string, error) {
		return `{"verdict":"approve","summary":"nothing to do","no_diff_expected":true}`, nil
	}
	arch := &LLMArchitect{Provider: mp}

	spec, err := arch.Review(context.Background(), ReviewInput{
		Task: "add an example", Round: 1, Diff: "", VerifyPassed: true, InvestigationOnly: false,
	})
	if err != nil {
		t.Fatalf("Review: %v", err)
	}
	if spec.Verdict != VerdictRevise {
		t.Fatalf("verdict = %q, want %q — an ordinary task must never approve an empty diff", spec.Verdict, VerdictRevise)
	}
}
