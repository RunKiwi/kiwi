# Live Anthropic LLM Provider Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the rule-based mock LLM with a live Anthropic (Claude) provider and add a real LLM Critic that reviews each proposed edit before it is applied, with tests as the final gate.

**Architecture:** A new `AnthropicProvider` implements both the existing Actor interface (`Provider.GetCodeEdit`) and a new `Critic` interface, calling `claude-opus-4-8` via the official Go SDK. The orchestrator engine runs Actor → Critic → (apply) → tests each iteration. The Anthropic API key is resolved through the existing reverse credential tunnel (with a daemon env-var fallback), and cost is computed from real token usage and fed into the existing budget gate. The mock provider/critic remain the default for offline and test runs.

**Tech Stack:** Go 1.21, `github.com/anthropics/anthropic-sdk-go`, GORM/SQLite, existing `sandbox` and `tunnel` packages.

## Global Constraints

- Module path: `github.com/ibreakthecloud/kiwi`.
- Go version floor: `go 1.21.4` (do not bump).
- Model ID: exactly `claude-opus-4-8` (typed constant `anthropic.ModelClaudeOpus4_8`) — never a date-suffixed variant.
- Thinking config: adaptive only (`anthropic.ThinkingConfigParamUnion{OfAdaptive: &anthropic.ThinkingConfigAdaptiveParam{}}`). Never send `budget_tokens`, `temperature`, `top_p`, or `top_k` (they 400 on Opus 4.8).
- Non-streaming requests only (keep `MaxTokens` ≤ 16000 to stay under SDK HTTP timeouts).
- Pricing constants (Opus 4.8): input `$5.00` / 1M tokens, output `$25.00` / 1M tokens.
- Never log the API key; never persist it to SQLite.
- Do not change the existing `Provider` interface signature (`GetCodeEdit`).
- Tests run with `CGO_ENABLED=0 go test -v ./pkg/...` (bypasses macOS dyld UUID checks).
- The live Anthropic path must never be exercised by unit tests (no network in CI).

---

### Task 1: Critic interface, Verdict type, UsageReporter, and MockCritic

**Files:**
- Create: `pkg/provider/critic.go`
- Test: `pkg/provider/critic_test.go`

**Interfaces:**
- Consumes: nothing (new file).
- Produces:
  - `type Verdict struct { Approved bool `json:"approved"`; Reasons string `json:"reasons"` }`
  - `type Critic interface { ReviewEdit(ctx context.Context, task, fileName, oldContent, newContent, buildOutput string) (Verdict, error) }`
  - `type UsageReporter interface { LastCostUSD() float64 }`
  - `type MockCritic struct{}` with `func NewMockCritic() *MockCritic` and `ReviewEdit` that always approves.

- [ ] **Step 1: Write the failing test**

```go
// pkg/provider/critic_test.go
package provider

import (
	"context"
	"testing"
)

func TestMockCriticAlwaysApproves(t *testing.T) {
	c := NewMockCritic()
	v, err := c.ReviewEdit(context.Background(), "task", "f.go", "old", "new", "boom")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !v.Approved {
		t.Fatalf("expected mock critic to approve, got %+v", v)
	}
}

// Compile-time guarantee the mock satisfies the interface.
var _ Critic = (*MockCritic)(nil)
```

- [ ] **Step 2: Run test to verify it fails**

Run: `CGO_ENABLED=0 go test ./pkg/provider/ -run TestMockCriticAlwaysApproves -v`
Expected: FAIL to compile — `undefined: NewMockCritic` / `undefined: Critic`.

- [ ] **Step 3: Write minimal implementation**

```go
// pkg/provider/critic.go
package provider

import "context"

// Verdict is the Critic's judgment on a proposed edit.
type Verdict struct {
	Approved bool   `json:"approved"`
	Reasons  string `json:"reasons"`
}

// Critic reviews a proposed edit before it is applied.
type Critic interface {
	ReviewEdit(ctx context.Context, task, fileName, oldContent, newContent, buildOutput string) (Verdict, error)
}

// UsageReporter is implemented by providers that can report the USD cost of
// their most recent API call, so the engine can enforce its budget.
type UsageReporter interface {
	LastCostUSD() float64
}

// MockCritic auto-approves every edit, for offline/test runs.
type MockCritic struct{}

func NewMockCritic() *MockCritic { return &MockCritic{} }

func (m *MockCritic) ReviewEdit(ctx context.Context, task, fileName, oldContent, newContent, buildOutput string) (Verdict, error) {
	return Verdict{Approved: true, Reasons: "mock critic auto-approves"}, nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `CGO_ENABLED=0 go test ./pkg/provider/ -run TestMockCriticAlwaysApproves -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add pkg/provider/critic.go pkg/provider/critic_test.go
git commit -m "feat(provider): add Critic interface, Verdict, UsageReporter, and MockCritic"
```

---

### Task 2: Pure parsing & pricing helpers (no SDK dependency)

**Files:**
- Create: `pkg/provider/parse.go`
- Test: `pkg/provider/parse_test.go`

**Interfaces:**
- Consumes: `Verdict` (Task 1).
- Produces:
  - `func extractCode(s string) string` — returns the contents of the first fenced code block, or the whole trimmed string if there is no fence.
  - `func parseVerdict(s string) Verdict` — extracts the first `{...}` JSON object and unmarshals it; on failure returns `Verdict{Approved:false, Reasons: "could not parse critic verdict: ..."}`.
  - `func costUSD(inputTokens, outputTokens int64) float64` — token cost at Opus 4.8 pricing.
  - Constants `inputCostPerMTok = 5.00`, `outputCostPerMTok = 25.00`.

- [ ] **Step 1: Write the failing test**

```go
// pkg/provider/parse_test.go
package provider

import (
	"math"
	"testing"
)

func TestExtractCode(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"plain fence", "here:\n```\nhello\n```\ndone", "hello"},
		{"lang-tagged fence", "```go\npackage x\n```", "package x"},
		{"no fence", "  just text  ", "just text"},
		{"first of two", "```\nA\n```\nmid\n```\nB\n```", "A"},
	}
	for _, c := range cases {
		if got := extractCode(c.in); got != c.want {
			t.Errorf("%s: extractCode()=%q want %q", c.name, got, c.want)
		}
	}
}

func TestParseVerdict(t *testing.T) {
	if v := parseVerdict(`{"approved": true, "reasons": "looks good"}`); !v.Approved || v.Reasons != "looks good" {
		t.Errorf("clean json: got %+v", v)
	}
	if v := parseVerdict("Sure!\n{\"approved\": false, \"reasons\": \"unsafe\"}\nthanks"); v.Approved || v.Reasons != "unsafe" {
		t.Errorf("embedded json: got %+v", v)
	}
	if v := parseVerdict("no json here"); v.Approved {
		t.Errorf("malformed must reject, got %+v", v)
	}
}

func TestCostUSD(t *testing.T) {
	// 1M input + 1M output = 5 + 25 = 30
	if got := costUSD(1_000_000, 1_000_000); math.Abs(got-30.0) > 1e-9 {
		t.Errorf("costUSD=%v want 30", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `CGO_ENABLED=0 go test ./pkg/provider/ -run 'TestExtractCode|TestParseVerdict|TestCostUSD' -v`
Expected: FAIL to compile — `undefined: extractCode` etc.

- [ ] **Step 3: Write minimal implementation**

```go
// pkg/provider/parse.go
package provider

import (
	"encoding/json"
	"strings"
)

const (
	inputCostPerMTok  = 5.00
	outputCostPerMTok = 25.00
)

// extractCode returns the contents of the first fenced code block in s.
// If there is no fence, it returns the whole string trimmed.
func extractCode(s string) string {
	start := strings.Index(s, "```")
	if start == -1 {
		return strings.TrimSpace(s)
	}
	rest := s[start+3:]
	// Drop the remainder of the fence line (optional language tag).
	if nl := strings.IndexByte(rest, '\n'); nl != -1 {
		rest = rest[nl+1:]
	}
	if end := strings.Index(rest, "```"); end != -1 {
		return strings.TrimSpace(rest[:end])
	}
	return strings.TrimSpace(rest)
}

// parseVerdict extracts the first JSON object from s and unmarshals it into a
// Verdict. Any failure is treated as a rejection (fail safe).
func parseVerdict(s string) Verdict {
	start := strings.IndexByte(s, '{')
	end := strings.LastIndexByte(s, '}')
	if start == -1 || end == -1 || end < start {
		return Verdict{Approved: false, Reasons: "could not parse critic verdict: no JSON object found"}
	}
	var v Verdict
	if err := json.Unmarshal([]byte(s[start:end+1]), &v); err != nil {
		return Verdict{Approved: false, Reasons: "could not parse critic verdict: " + err.Error()}
	}
	return v
}

// costUSD computes the cost of a call given token usage at Opus 4.8 pricing.
func costUSD(inputTokens, outputTokens int64) float64 {
	return float64(inputTokens)/1e6*inputCostPerMTok + float64(outputTokens)/1e6*outputCostPerMTok
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `CGO_ENABLED=0 go test ./pkg/provider/ -run 'TestExtractCode|TestParseVerdict|TestCostUSD' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add pkg/provider/parse.go pkg/provider/parse_test.go
git commit -m "feat(provider): add fenced-code, verdict, and pricing helpers"
```

---

### Task 3: AnthropicProvider (Actor + Critic) via the Go SDK

**Files:**
- Create: `pkg/provider/llm.go`
- Test: `pkg/provider/llm_test.go`
- Modify: `go.mod`, `go.sum` (via `go get` / `go mod tidy`)

**Interfaces:**
- Consumes: `Provider` (from `mock.go`), `Critic`, `UsageReporter`, `Verdict` (Task 1), `extractCode`, `parseVerdict`, `costUSD`, pricing constants (Task 2).
- Produces:
  - `type AnthropicProvider struct { ... }`
  - `func NewAnthropicProvider(apiKey string) *AnthropicProvider`
  - Methods: `GetCodeEdit(...) (string, error)`, `ReviewEdit(...) (Verdict, error)`, `LastCostUSD() float64`.

- [ ] **Step 1: Add the SDK dependency**

Run:
```bash
go get github.com/anthropics/anthropic-sdk-go@latest
go mod tidy
```
Expected: `go.mod` gains `github.com/anthropics/anthropic-sdk-go`.

- [ ] **Step 2: Write the failing test (compile-time interface assertions + no network)**

```go
// pkg/provider/llm_test.go
package provider

import "testing"

// Compile-time proof AnthropicProvider satisfies all three interfaces.
var (
	_ Provider      = (*AnthropicProvider)(nil)
	_ Critic        = (*AnthropicProvider)(nil)
	_ UsageReporter = (*AnthropicProvider)(nil)
)

func TestNewAnthropicProviderConstructs(t *testing.T) {
	p := NewAnthropicProvider("test-key-not-used")
	if p == nil {
		t.Fatal("expected non-nil provider")
	}
	if p.LastCostUSD() != 0 {
		t.Fatalf("expected zero initial cost, got %v", p.LastCostUSD())
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `CGO_ENABLED=0 go test ./pkg/provider/ -run TestNewAnthropicProviderConstructs -v`
Expected: FAIL to compile — `undefined: AnthropicProvider` / `undefined: NewAnthropicProvider`.

- [ ] **Step 4: Write minimal implementation**

```go
// pkg/provider/llm.go
package provider

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

// AnthropicProvider is a live Claude-backed Actor and Critic.
type AnthropicProvider struct {
	client   anthropic.Client
	model    anthropic.Model
	lastCost float64
}

// NewAnthropicProvider builds a provider using the given API key.
func NewAnthropicProvider(apiKey string) *AnthropicProvider {
	return &AnthropicProvider{
		client: anthropic.NewClient(option.WithAPIKey(apiKey)),
		model:  anthropic.ModelClaudeOpus4_8,
	}
}

// LastCostUSD reports the USD cost of the most recent API call.
func (p *AnthropicProvider) LastCostUSD() float64 { return p.lastCost }

func (p *AnthropicProvider) recordCost(u anthropic.Usage) {
	p.lastCost = costUSD(u.InputTokens, u.OutputTokens)
}

func collectText(resp *anthropic.Message) string {
	var b strings.Builder
	for _, block := range resp.Content {
		if t, ok := block.AsAny().(anthropic.TextBlock); ok {
			b.WriteString(t.Text)
		}
	}
	return b.String()
}

// GetCodeEdit is the Actor: propose the complete corrected file.
func (p *AnthropicProvider) GetCodeEdit(ctx context.Context, task, fileName, codeContent, buildOutput string) (string, error) {
	system := "You are an expert Go engineer acting as the Actor in an automated fix loop. " +
		"Given a failing file and its build/test output, make the SMALLEST change that makes the tests pass. " +
		"Do not refactor unrelated code. Respond with the COMPLETE corrected file inside a single fenced code block."

	user := fmt.Sprintf("Task: %s\n\nFile: %s\n\nCurrent contents:\n```\n%s\n```\n\nBuild/test output:\n%s",
		task, fileName, codeContent, buildOutput)

	adaptive := anthropic.ThinkingConfigAdaptiveParam{}
	resp, err := p.client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     p.model,
		MaxTokens: 16000,
		System:    []anthropic.TextBlockParam{{Text: system}},
		Thinking:  anthropic.ThinkingConfigParamUnion{OfAdaptive: &adaptive},
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(user)),
		},
	})
	if err != nil {
		return "", fmt.Errorf("anthropic actor request failed: %w", err)
	}
	p.recordCost(resp.Usage)
	if resp.StopReason == anthropic.StopReasonRefusal {
		return "", errors.New("actor request refused by safety classifier")
	}
	return extractCode(collectText(resp)), nil
}

// ReviewEdit is the Critic: judge the proposed change before it is applied.
func (p *AnthropicProvider) ReviewEdit(ctx context.Context, task, fileName, oldContent, newContent, buildOutput string) (Verdict, error) {
	system := "You are the Critic in an automated fix loop. Review the proposed change for correctness and safety. " +
		"Approve only if it is a plausible, safe fix for the stated task. " +
		`Respond ONLY with a JSON object: {"approved": bool, "reasons": string}.`

	user := fmt.Sprintf("Task: %s\n\nFile: %s\n\nOriginal:\n```\n%s\n```\n\nProposed:\n```\n%s\n```\n\nBuild/test output that motivated the change:\n%s",
		task, fileName, oldContent, newContent, buildOutput)

	adaptive := anthropic.ThinkingConfigAdaptiveParam{}
	resp, err := p.client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     p.model,
		MaxTokens: 2000,
		System:    []anthropic.TextBlockParam{{Text: system}},
		Thinking:  anthropic.ThinkingConfigParamUnion{OfAdaptive: &adaptive},
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(user)),
		},
	})
	if err != nil {
		return Verdict{}, fmt.Errorf("anthropic critic request failed: %w", err)
	}
	p.recordCost(resp.Usage)
	if resp.StopReason == anthropic.StopReasonRefusal {
		return Verdict{Approved: false, Reasons: "critic refused to review (safety classifier)"}, nil
	}
	return parseVerdict(collectText(resp)), nil
}
```

> **Compile-fix note:** the SDK symbol names above (`anthropic.Client`, `anthropic.Usage`, `.InputTokens`/`.OutputTokens`, `anthropic.StopReasonRefusal`, `anthropic.TextBlock`, `block.AsAny()`) are taken from the official Go SDK reference. If any name differs in the installed version, let the compiler error point you to the correct symbol (e.g. `strings`/`jar`-style discovery is unnecessary — `go build` names the exact identifier). Do not change interface signatures to work around a mismatch.

- [ ] **Step 5: Run test to verify it passes**

Run: `CGO_ENABLED=0 go test ./pkg/provider/ -run TestNewAnthropicProviderConstructs -v`
Expected: PASS (and the package compiles).

- [ ] **Step 6: Commit**

```bash
git add pkg/provider/llm.go pkg/provider/llm_test.go go.mod go.sum
git commit -m "feat(provider): add live AnthropicProvider implementing Actor and Critic"
```

---

### Task 4: Engine — Critic step, feedback threading, API-key resolution, real cost

**Files:**
- Modify: `pkg/orchestrator/engine.go`
- Test: `pkg/orchestrator/engine_test.go`

**Interfaces:**
- Consumes: `provider.Provider`, `provider.Critic`, `provider.UsageReporter`, `provider.NewAnthropicProvider` (Task 3), `tunnel.GlobalRegistry`, `sandbox.RunCommand`.
- Produces (new fields/helpers on `Engine`):
  - Fields: `Critic provider.Critic`, `LLMMode string`.
  - `func composeActorInput(buildOutput, criticReasons string) string` (pure, testable).
  - `func (e *Engine) resolveAPIKey(ctx context.Context, taskID string) (string, error)`.
  - Revised `RunTask` loop: Actor → Critic → (apply on approve) → tests.

- [ ] **Step 1: Write the failing test (pure feedback-threading helper)**

```go
// pkg/orchestrator/engine_test.go
package orchestrator

import (
	"strings"
	"testing"
)

func TestComposeActorInput(t *testing.T) {
	// No critic feedback → just the build output.
	if got := composeActorInput("build failed", ""); got != "build failed" {
		t.Errorf("no feedback: got %q", got)
	}
	// With feedback → build output plus a clearly delimited critic note.
	got := composeActorInput("build failed", "you forgot the zero check")
	if !strings.Contains(got, "build failed") || !strings.Contains(got, "you forgot the zero check") {
		t.Errorf("with feedback: got %q", got)
	}
	if !strings.Contains(got, "Critic feedback") {
		t.Errorf("expected a labelled critic section, got %q", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `CGO_ENABLED=0 go test ./pkg/orchestrator/ -run TestComposeActorInput -v`
Expected: FAIL to compile — `undefined: composeActorInput`.

- [ ] **Step 3: Add fields, helpers, and the revised loop**

In `pkg/orchestrator/engine.go`, add the imports `provider` (already present) and keep `tunnel`, `os`, `time`, `context`, `fmt`.

Add the two new fields to the `Engine` struct:

```go
type Engine struct {
	Provider      provider.Provider
	Critic        provider.Critic
	MaxSteps      int
	LogOut        io.Writer
	StateCallback func(string)
	CostCallback  func(float64)
	MaxBudget     float64
	LLMMode       string // "mock" (default) or "anthropic"
}
```

Add these helpers (anywhere below `resolveEnv`):

```go
// composeActorInput appends any Critic feedback to the build output so the
// Actor sees why its previous attempt was rejected. Signatures are fixed, so
// feedback rides along inside the buildOutput argument.
func composeActorInput(buildOutput, criticReasons string) string {
	if strings.TrimSpace(criticReasons) == "" {
		return buildOutput
	}
	return buildOutput + "\n\n[Critic feedback on your previous attempt]: " + criticReasons
}

// charge attributes the cost of the most recent call by `caller` to the budget.
// Live providers report real token cost via UsageReporter; the mock uses a
// small nominal fallback so the budget path stays exercised offline.
func (e *Engine) charge(acc *float64, caller interface{}, fallback float64) {
	cost := fallback
	if r, ok := caller.(provider.UsageReporter); ok {
		cost = r.LastCostUSD()
	}
	*acc += cost
	if e.CostCallback != nil {
		e.CostCallback(cost)
	}
}

// resolveAPIKey resolves ANTHROPIC_API_KEY through the reverse tunnel (cache
// first), falling back to the daemon environment. If neither is available it
// pauses statefully until the client reconnects.
func (e *Engine) resolveAPIKey(ctx context.Context, taskID string) (string, error) {
	t := tunnel.GlobalRegistry.Get(taskID)
	for {
		if t != nil {
			if val, err := t.GetSecret(ctx, "ANTHROPIC_API_KEY"); err == nil && val != "" {
				if e.StateCallback != nil {
					e.StateCallback("RUNNING")
				}
				return val, nil
			}
		}
		if val := os.Getenv("ANTHROPIC_API_KEY"); val != "" {
			return val, nil
		}
		e.log("[Orchestrator] ANTHROPIC_API_KEY unavailable. Pausing until client reconnects...\n")
		if e.StateCallback != nil {
			e.StateCallback("PAUSED")
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(1 * time.Second):
		}
	}
}
```

Add `"strings"` to the import block of `engine.go`.

Near the top of `RunTask`, before the initial test run, lazily build the live provider:

```go
// Build the live provider on demand (key resolved via tunnel / env).
if e.LLMMode == "anthropic" && e.Provider == nil {
	key, err := e.resolveAPIKey(ctx, taskID)
	if err != nil {
		return fmt.Errorf("failed to resolve ANTHROPIC_API_KEY: %w", err)
	}
	ap := provider.NewAnthropicProvider(key)
	e.Provider = ap
	e.Critic = ap
}
```

Replace the initial-test nominal cost block (`accumulatedCost += 0.02` and its `CostCallback`) — keep it as-is; the initial run is not an LLM call.

Replace the correction loop body (current lines ~115–175) with:

```go
	lastBuildOutput := res.Output
	criticReasons := ""

	for step := 1; step <= e.MaxSteps; step++ {
		e.log("\n=== Loop Iteration %d / %d ===\n", step, e.MaxSteps)

		if accumulatedCost >= e.MaxBudget {
			e.log("[Orchestrator Halt] Loop terminated: task budget limit ($%.2f) reached.\n", e.MaxBudget)
			return fmt.Errorf("loop halted: budget limit ($%.2f) exceeded", e.MaxBudget)
		}

		content, err := os.ReadFile(filePath)
		if err != nil {
			return fmt.Errorf("failed to read target file: %w", err)
		}

		// Actor proposes (not yet written).
		e.log("[Actor] Proposing edit...\n")
		proposed, err := e.Provider.GetCodeEdit(ctx, task, filePath, string(content), composeActorInput(lastBuildOutput, criticReasons))
		if err != nil {
			return fmt.Errorf("failed to get code edit: %w", err)
		}
		e.charge(&accumulatedCost, e.Provider, 0.05)
		criticReasons = ""

		// Critic reviews before applying.
		verdict := provider.Verdict{Approved: true, Reasons: "no critic configured"}
		if e.Critic != nil {
			verdict, err = e.Critic.ReviewEdit(ctx, task, filePath, string(content), proposed, lastBuildOutput)
			if err != nil {
				return fmt.Errorf("critic review failed: %w", err)
			}
			e.charge(&accumulatedCost, e.Critic, 0.02)
		}

		if !verdict.Approved {
			e.log("[Critic] Rejected edit: %s\n", verdict.Reasons)
			criticReasons = verdict.Reasons
			continue // do not apply, do not run tests; Actor retries with feedback
		}
		e.log("[Critic] Approved edit.\n")

		// Apply the approved edit.
		if err := os.WriteFile(filePath, []byte(proposed), 0644); err != nil {
			return fmt.Errorf("failed to write fix back to target file: %w", err)
		}
		e.log("[Actor] Applied approved edit to target file.\n")

		// Final gate: run the tests.
		e.log("[Sandbox] Re-running build/tests...\n")
		env, err = e.resolveEnv(ctx, taskID, []string{"GITHUB_TOKEN"})
		if err != nil {
			return fmt.Errorf("failed to resolve tunnel environment: %w", err)
		}
		res, err = sandbox.RunCommand(ctx, dir, testCmd, env)
		if err != nil {
			return fmt.Errorf("sandbox run failed: %w", err)
		}

		if res.Success {
			e.log("[Gate] Success: tests passed.\n")
			return nil
		}

		e.log("[Gate] Fail: target state still diverging.\n")
		e.log("[Sandbox Output]:\n%s\n", res.Output)
		lastBuildOutput = res.Output

		outputCounts[res.Output]++
		if outputCounts[res.Output] >= 3 {
			e.log("[Orchestrator Halt] Loop safety cut-off: recursive loop detected (compiler error repeated 3 times).\n")
			return fmt.Errorf("loop safety halt: recursive loop detected (compiler error repeated 3 times)")
		}
	}

	return fmt.Errorf("loop halted: reached max iterations (%d) without resolving task", e.MaxSteps)
```

- [ ] **Step 4: Run the helper test to verify it passes**

Run: `CGO_ENABLED=0 go test ./pkg/orchestrator/ -run TestComposeActorInput -v`
Expected: PASS.

- [ ] **Step 5: Run the full package test suite**

Run: `CGO_ENABLED=0 go test ./pkg/... -v`
Expected: PASS (mock path still compiles and behaves; provider helpers green).

- [ ] **Step 6: Commit**

```bash
git add pkg/orchestrator/engine.go pkg/orchestrator/engine_test.go
git commit -m "feat(engine): add LLM Critic step, feedback threading, key resolution, real cost"
```

---

### Task 5: Wire provider selection, timeout, and budget in the daemon

**Files:**
- Modify: `pkg/orchestrator/server.go` (task goroutine around lines 204–217)

**Interfaces:**
- Consumes: `provider.NewMockProvider`, `provider.NewMockCritic`, `Engine`, `NewEngine`, `os.Getenv`.
- Produces: environment-driven configuration — `KIWI_LLM_PROVIDER` (`mock` default | `anthropic`), `KIWI_TASK_TIMEOUT` (Go duration, default `10m`), `KIWI_MAX_BUDGET` (float, default `1.00`).

- [ ] **Step 1: Add a config helper near the top of `server.go`**

Add these imports if missing: `"strconv"` (and `"time"`, already present). Add a helper function in `server.go`:

```go
// taskTimeout returns the per-task context timeout from KIWI_TASK_TIMEOUT
// (Go duration string), defaulting to 10 minutes.
func taskTimeout() time.Duration {
	if v := os.Getenv("KIWI_TASK_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return 10 * time.Minute
}

// maxBudget returns the per-task USD budget from KIWI_MAX_BUDGET, defaulting to $1.00.
func maxBudget() float64 {
	if v := os.Getenv("KIWI_MAX_BUDGET"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return 1.00
}
```

- [ ] **Step 2: Replace the engine construction block**

Replace:

```go
		p := provider.NewMockProvider()
		engine := NewEngine(p, 5)
```

with:

```go
		var engine *Engine
		if os.Getenv("KIWI_LLM_PROVIDER") == "anthropic" {
			engine = NewEngine(nil, 5) // provider built lazily after key resolution
			engine.LLMMode = "anthropic"
		} else {
			engine = NewEngine(provider.NewMockProvider(), 5)
			engine.Critic = provider.NewMockCritic()
			engine.LLMMode = "mock"
		}
		engine.MaxBudget = maxBudget()
```

- [ ] **Step 3: Replace the hard-coded task timeout**

Replace:

```go
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
```

with:

```go
		ctx, cancel := context.WithTimeout(context.Background(), taskTimeout())
```

- [ ] **Step 4: Build the daemon to verify it compiles**

Run: `CGO_ENABLED=0 go build ./...`
Expected: no output (success).

- [ ] **Step 5: Run the full test suite**

Run: `CGO_ENABLED=0 go test ./pkg/... -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add pkg/orchestrator/server.go
git commit -m "feat(daemon): select LLM provider, timeout, and budget via env vars"
```

---

### Task 6: Build binaries and manual end-to-end verification

**Files:** none (build + docs only).

**Interfaces:** none.

- [ ] **Step 1: Build and sign both binaries (macOS workaround per CLAUDE.md)**

Run:
```bash
go build -ldflags="-linkmode=external" -o kiwi cmd/kiwi/main.go && codesign -s - -f ./kiwi
go build -ldflags="-linkmode=external" -o kiwid cmd/kiwid/main.go && codesign -s - -f ./kiwid
```
Expected: two binaries built and signed, no errors.

- [ ] **Step 2: Verify mock mode still works (no key, offline)**

Run (in one shell):
```bash
export KIWI_SERVER_TOKEN="my-secret-token-1234"
./kiwid -addr :8080 -db kiwi.db
```
Run (in another shell):
```bash
./kiwi -token "my-secret-token-1234" -task "Fix division by zero in Divide()" -file demo_project/math_utils.go -test-cmd "go test ./demo_project/..."
```
Expected: task reaches SUCCESS via the mock Actor/Critic path (Critic logs "Approved edit").

- [ ] **Step 3: Verify live Anthropic mode (requires a real key)**

Run (daemon shell):
```bash
export KIWI_SERVER_TOKEN="my-secret-token-1234"
export KIWI_LLM_PROVIDER="anthropic"
export ANTHROPIC_API_KEY="sk-ant-..."   # env fallback; tunnel also supported
./kiwid -addr :8080 -db kiwi.db
```
Run (client shell): same `./kiwi ...` command as Step 2.
Expected: logs show real `[Actor] Proposing edit...` and `[Critic] Approved edit.`/`Rejected edit`, a real fix is applied, tests pass, and the task cost reflects token usage (non-zero, budget-bounded).

- [ ] **Step 4: Update CLAUDE.md status**

Edit `CLAUDE.md`:
- Move "Mock AI Provider" out of Active Limitations (now superseded by the live Anthropic provider; note OpenAI + per-role model swapping remain TODO).
- Check off the relevant boxes under "Integration of Live LLM Providers" and add a new completed phase entry describing the Anthropic Actor–Critic integration and tunnel-resolved API key.

- [ ] **Step 5: Commit**

```bash
git add CLAUDE.md
git commit -m "docs: mark live Anthropic Actor-Critic provider complete"
```

---

## Self-Review

**Spec coverage:**
- Anthropic-first pluggable provider → Task 3. ✅
- Real LLM Critic reviewing before tests → Tasks 1 (interface) + 3 (impl) + 4 (loop). ✅
- API key via tunnel + env fallback + pause → Task 4 `resolveAPIKey`. ✅
- Token-based cost into budget gate → Tasks 2 (`costUSD`) + 3 (`recordCost`) + 4 (`charge`). ✅
- Provider selection env var, mock default → Task 5. ✅
- Timeout bump, configurable budget → Task 5. ✅
- Mock stays default; offline tests green → Tasks 1, 4, 5. ✅
- Unit tests for extraction/verdict/pricing → Task 2. ✅
- No-network guarantee for live path → Task 3 (compile-time assertions only). ✅
- Model/thinking/pricing constants verbatim → Global Constraints. ✅

**Placeholder scan:** No TBD/TODO/"handle edge cases" — every code step shows full code; every run step shows the command and expected result.

**Type consistency:** `Verdict{Approved, Reasons}`, `Critic.ReviewEdit(...)`, `UsageReporter.LastCostUSD()`, `AnthropicProvider`, `NewAnthropicProvider`, `composeActorInput`, `charge`, `resolveAPIKey`, `LLMMode` are used identically across Tasks 1–5. `Engine.Provider`/`Engine.Critic` set in Task 5 match fields added in Task 4.
