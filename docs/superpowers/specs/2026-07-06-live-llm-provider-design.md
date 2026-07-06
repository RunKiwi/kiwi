# Design: Live Anthropic LLM Provider with Actor–Critic Loop

**Date:** 2026-07-06
**Status:** Approved (pending spec review)
**Author:** Kiwi cofounders (pairing session)

## Goal

Replace Kiwi's rule-based mock LLM (`pkg/provider/mock.go`) with a live Anthropic
(Claude) integration, and make the "Actor–Critic" branding real by adding a genuine
LLM Critic that reviews each proposed edit before it is applied. Tests remain the
final gate. This is the single most convincing capability for a demo: an end-to-end
run where Kiwi actually fixes real code.

## Scope

**In scope**
- Anthropic-first provider behind a pluggable interface (`claude-opus-4-8`).
- A real LLM Critic that reviews the Actor's diff and can reject it before tests run.
- Anthropic API key resolved through the existing reverse credential tunnel, with a
  daemon environment-variable fallback. Key is never persisted server-side and never
  logged.
- Token-based cost accounting driving the existing budget gate.
- Provider selection via environment variable; mock remains the default for
  offline/test use.

**Out of scope (deliberately, behind the same interfaces for a later pass)**
- OpenAI / GPT integration.
- Dynamic per-role model swapping (different model for Actor vs Critic).
- `kiwi login` / OAuth / JWT.

## Non-negotiable API facts (from the authoritative claude-api reference)

- Model ID: `claude-opus-4-8` (exact string, no date suffix).
- Go SDK: `github.com/anthropics/anthropic-sdk-go`; client via
  `anthropic.NewClient(option.WithAPIKey(key))`; request via
  `client.Messages.New(ctx, anthropic.MessageNewParams{...})`.
- Thinking: adaptive only —
  `anthropic.ThinkingConfigParamUnion{OfAdaptive: &anthropic.ThinkingConfigAdaptiveParam{}}`.
  `budget_tokens` and sampling params (`temperature`/`top_p`/`top_k`) are removed on
  Opus 4.8 and return 400 — do not send them.
- Stream when `max_tokens` is large (> ~16K) to avoid SDK HTTP timeouts; use
  `stream.Accumulate(...)` to build the final message (Go has no `GetFinalMessage`).
- Handle `stop_reason == "refusal"` before reading `resp.Content` (guard against
  empty content).
- Pricing for cost accounting: Opus 4.8 = $5.00 / 1M input tokens,
  $25.00 / 1M output tokens.

## Architecture

### Provider interfaces (`pkg/provider/`)

The existing Actor interface is unchanged. A new Critic interface makes the review
role explicit.

```go
// Actor — existing, signature unchanged.
type Provider interface {
    GetCodeEdit(ctx context.Context, task, fileName, codeContent, buildOutput string) (string, error)
}

// Critic — new.
type Critic interface {
    ReviewEdit(ctx context.Context, task, fileName, oldContent, newContent, buildOutput string) (Verdict, error)
}

type Verdict struct {
    Approved bool   `json:"approved"`
    Reasons  string `json:"reasons"` // fed back to the Actor on rejection
}
```

### `pkg/provider/llm.go` (new)

`AnthropicProvider` implements **both** `Provider` and `Critic`.

- Constructed with a resolved API key and a model string (default `claude-opus-4-8`).
- Holds one `anthropic.Client`.

**Actor (`GetCodeEdit`)**
- System prompt: role = fix the compiler/test failure with the smallest change that
  makes tests pass; do not refactor unrelated code; return the complete corrected
  file.
- User message: task description, target filename, full current file content, and the
  latest build/test output. On a retry after a Critic rejection, the Critic's
  `Reasons` are appended to the user message.
- Output contract: the model returns the full corrected file in a single fenced code
  block. We extract the fenced block (`extractCode`). If no fence is present, treat
  the whole response as the file body. (Structured outputs via `output_config.format`
  is a possible future upgrade once the Go binding is verified; fenced extraction is
  the initial, dependency-free approach.)

**Critic (`ReviewEdit`)**
- System prompt: role = review the proposed diff for correctness and safety; approve
  only if the change is a plausible, safe fix for the stated task; otherwise reject
  with specific reasons.
- User message: task, filename, old content, new content (or a computed unified diff),
  and the build/test output that motivated the edit.
- Output contract: a small JSON object `{"approved": bool, "reasons": string}` parsed
  with `encoding/json` (lenient extraction of the first JSON object in the response).

**Shared concerns**
- `max_tokens` sized for a full file (stream when large).
- Refusal handling: a refusal from the Actor returns an error that surfaces as a task
  failure; a refusal from the Critic is treated as a rejection with a reason.
- Token usage from `resp.Usage` returned alongside results so the engine can compute
  cost. (Implementation detail: either return usage via an out-param/struct, or expose
  a `LastUsage()` accessor. Chosen in the plan; must not change the public
  `Provider`/`Critic` signatures — usage flows through the concrete type or a small
  cost hook.)

### `pkg/provider/mock.go`

- Keep `MockProvider` (Actor) as-is.
- Add `MockCritic` implementing `Critic` with an always-approve `Verdict` so
  offline mode and the existing mock end-to-end path continue to pass with a Critic
  step wired in.

### Engine (`pkg/orchestrator/engine.go`)

Add a `Critic provider.Critic` field to `Engine`. Revised per-iteration flow inside
the existing `for step := 1; step <= e.MaxSteps` loop:

1. Read current file content.
2. **Actor** proposes new content (not yet written). On a retry after rejection, pass
   the prior Critic reasons.
3. **Critic** reviews old→new.
   - Rejected → log the reasons, do **not** write, do **not** run tests; continue to
     the next loop step (Actor sees the reasons). Consumes budget.
   - Approved → write the file, then run tests.
4. Tests are the **final gate**: pass → success; fail → continue with new test output.

**Loop bounding:** each Actor call and each Critic call adds real token cost via
`CostCallback`, and each step increments toward `MaxSteps`. The existing
`MaxBudget`, `MaxSteps`, and duplicate-output cutoff bound any Actor↔Critic
ping-pong. No new retry knob is introduced (decided during brainstorming).

### API key resolution (reuse `resolveEnv`)

- Extend the credential resolution to also resolve `ANTHROPIC_API_KEY` through the
  reverse tunnel, using the same cache-first, pause-on-disconnect behavior already
  implemented for `GITHUB_TOKEN` in `Engine.resolveEnv`.
- Fallback order: tunnel-resolved value → daemon `ANTHROPIC_API_KEY` env var. If
  neither is available, the task **PAUSES** (stateful, same as a tunnel
  disconnect) until the client reconnects.
- The resolved key constructs the `AnthropicProvider` at task runtime. It is held in
  memory only, never written to SQLite, never logged.

### Cost accounting

- Remove the hard-coded `+= 0.02` / `+= 0.05`.
- Add a pricing helper: `cost = inputTokens/1e6 * 5.00 + outputTokens/1e6 * 25.00`.
- Feed the per-call cost to the existing `CostCallback`; budget gating is unchanged.
- In mock mode, retain small nominal costs so the budget path stays exercised.

### Wiring & config (`pkg/orchestrator/server.go`)

- Select provider by env var `KIWI_LLM_PROVIDER`:
  - unset or `mock` → `MockProvider` + `MockCritic` (default; offline/tests unchanged).
  - `anthropic` → `AnthropicProvider` as both Actor and Critic.
- Set `engine.Critic` alongside `engine.Provider`.
- **Raise the task context timeout** from 2 minutes to ~10 minutes (real LLM
  iterations are slower); make it configurable via env var with a sane default.

## Data flow (anthropic mode)

```
POST /tasks → task row (RUNNING) → engine.RunTask
  resolveEnv: ANTHROPIC_API_KEY (tunnel cache → env fallback → PAUSE if absent)
  construct AnthropicProvider(key)
  initial test run (final gate check)
  loop (bounded by MaxSteps / MaxBudget / dup-output):
    Actor.GetCodeEdit → new file content            [cost: input+output tokens]
    Critic.ReviewEdit → Verdict                      [cost: input+output tokens]
      reject → next step with reasons (no write, no test)
      approve → write file → sandbox test
        pass → SUCCESS
        fail → next step with test output
```

## Error handling

- Actor refusal or API error → task FAILED with a clear message (no secret leakage).
- Critic refusal → treated as rejection with the refusal explanation as `Reasons`.
- Fenced-code extraction miss → fall back to raw response body as file content.
- JSON verdict parse failure → treat as rejection with a "could not parse verdict"
  reason (fail safe: never silently apply an unreviewed edit).
- Rate limit / 5xx → the Go SDK retries with backoff by default; a persistent failure
  surfaces as a task error.

## Testing

- Unit tests (no network):
  - `extractCode`: fenced block, no fence, multiple fences (take first/largest),
    language-tagged fences.
  - Critic verdict parsing: clean JSON, JSON embedded in prose, malformed JSON
    (→ reject).
  - Pricing helper arithmetic.
- Mock-mode end-to-end path stays green with the new Critic step:
  `CGO_ENABLED=0 go test -v ./pkg/...`.
- Live Anthropic path is guarded so tests never hit the network without an explicit
  key/opt-in.

## Build / verification

- Build both binaries per CLAUDE.md (external linkmode + ad-hoc codesign on macOS).
- `go mod tidy` after adding the Anthropic SDK dependency.
- Manual demo: run daemon with `KIWI_LLM_PROVIDER=anthropic`, submit the
  `demo_project` division-by-zero task, confirm a real fix and passing tests.

## Risks / notes

- Go SDK exact symbol names (thinking union, usage fields, streaming accumulate) are
  verified against the claude-api reference but must be confirmed at compile time; the
  plan includes a compile-fix step rather than pre-emptive research.
- Real runs cost real money; `MaxBudget` (default $0.20) and the raised-but-bounded
  timeout are the guardrails. Keep the default budget conservative.
