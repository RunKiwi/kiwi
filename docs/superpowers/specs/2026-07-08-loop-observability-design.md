# Design: Actor–Critic Loop Observability

**Date:** 2026-07-08
**Status:** Approved (pending spec review)
**Author:** Kiwi cofounders (pairing session)

## Goal

Replace the loop's freeform log lines with **structured, queryable per-iteration
telemetry**: for each phase of the Actor–Critic loop, capture timing, token
usage, cost, and outcome as rows in a new `task_events` table, exposed via
`GET /tasks/{id}/events`. This makes a run legible — you can see cost/latency and
the decision trace per step — which is the natural companion to the live Claude
loop and a strong demo asset.

## Context (current `main`, post multi-tenant foundation)

The codebase now has `pkg/auth` (JWT/claims), org/user scoping on `TaskState`
(`UserID`, `OrgID`, `UserEmail`), an auth middleware wrapping all routes
(`auth.ClaimsFromContext`), and `authorizeTask(claims, task)` (same-org or admin).
Observability must respect this: **task events are tenant data and the endpoint
must enforce the same authorization as `handleTaskStatus`.**

Engine, server, and db are all `package orchestrator`, so the event type is shared
directly — no cross-package plumbing.

## Scope

**In scope**
- `provider.TokenReporter` + `AnthropicProvider.LastUsage()` (input/output tokens).
- `TaskEvent` GORM model + `AutoMigrate`.
- `Engine.EventCallback` and per-phase emission in `RunTask` (additive; existing
  `e.log` lines stay).
- Persist events in `launchTask`'s `EventCallback` (stamping `OrgID` from the task).
- `GET /tasks/{id}/events`, authorized via the parent task (`authorizeTask`).

**Out of scope (deliberately)**
- Dashboard timeline UI (ships data + API; the web view is a fast follow).
- OpenTelemetry/tracing export; cross-task aggregate metrics/rollups.
- Instrumenting `RecoverTasks` beyond the events its re-launched `RunTask` emits.

## Architecture

### 1. Provider token reporting (`pkg/provider/critic.go`, `llm.go`)

Add alongside the existing `UsageReporter`:
```go
type TokenReporter interface { LastUsage() (inputTokens, outputTokens int64) }
```
`AnthropicProvider` already has `recordCost(u anthropic.Usage)` storing `lastCost`.
Extend it to also store `lastInput`/`lastOutput` and implement `LastUsage()`. The
mock does not implement `TokenReporter` → tokens report `0`. No existing signatures
change (`UsageReporter`/`Provider`/`Critic` untouched).

### 2. Event model (`pkg/orchestrator/events.go`)

```go
type TaskEvent struct {
    ID           uint      `json:"id" gorm:"primaryKey;autoIncrement"`
    TaskID       string    `json:"task_id" gorm:"index"`
    OrgID        string    `json:"org_id" gorm:"index"`
    Step         int       `json:"step"`          // 0 = initial test; 1..N = iterations
    Phase        string    `json:"phase"`         // initial_test | actor | critic | test
    Outcome      string    `json:"outcome"`       // pass | fail | approved | rejected | error
    Detail       string    `json:"detail"`        // verdict reasons / test-output summary
    DurationMs   int64     `json:"duration_ms"`
    InputTokens  int64     `json:"input_tokens"`
    OutputTokens int64     `json:"output_tokens"`
    CostUSD      float64   `json:"cost_usd"`
    CreatedAt    time.Time `json:"created_at"`
}
```
Added to `InitDB` `AutoMigrate`. `OrgID` is stamped by the server at insert time
(the engine does not know it); it aids org-wide analytics and defense-in-depth.

### 3. Engine emission (`pkg/orchestrator/engine.go`)

- Add `EventCallback func(TaskEvent)` to `Engine`.
- Add a helper:
```go
func (e *Engine) emit(step int, phase, outcome, detail string, dur time.Duration, caller interface{}) {
    if e.EventCallback == nil { return }
    ev := TaskEvent{Step: step, Phase: phase, Outcome: outcome, Detail: detail, DurationMs: dur.Milliseconds()}
    if r, ok := caller.(provider.UsageReporter); ok { ev.CostUSD = r.LastCostUSD() }
    if tr, ok := caller.(provider.TokenReporter); ok { ev.InputTokens, ev.OutputTokens = tr.LastUsage() }
    e.EventCallback(ev)
}
```
- Instrument each phase by wrapping it with `time.Now()`:
  - **initial_test** (step 0): outcome `pass`/`fail`, detail = summarized output, `caller=nil` (no tokens).
  - **actor** (step N): after `GetCodeEdit`, outcome `proposed` on success or `error`; tokens/cost from `e.Provider`.
  - **critic** (step N): after `ReviewEdit`, outcome `approved`/`rejected`, detail = `verdict.Reasons`; tokens/cost from `e.Critic`.
  - **test** (step N): after the re-run, outcome `pass`/`fail`, detail = summarized output, `caller=nil`.
- `charge` is unchanged (budget accounting); `emit` reads the same
  `LastCostUSD()` value immediately after the same call, so cost is consistent.
- Detail summaries are truncated (e.g. last ~500 chars of test output) to keep rows
  small; a shared `summarize(s string, n int)` helper.

### 4. Persistence + wiring (`pkg/orchestrator/server.go`)

In `launchTask`, set the callback (capturing the task's `OrgID` from the `existing`
row already loaded there):
```go
orgID := existing.OrgID
engine.EventCallback = func(ev TaskEvent) {
    ev.TaskID = taskID
    ev.OrgID = orgID
    s.db.Create(&ev)
}
```

### 5. Events endpoint (`pkg/orchestrator/server.go`)

Route `GET /tasks/{id}/events` through the existing `handleTaskStatus` handler by
detecting the `/events` suffix (mirrors the tunnel's `/response` suffix pattern):
- Parse `taskID` = the path segment before `/events`.
- Load the `TaskState`; 404 if missing.
- `authorizeTask(claims, &state)` — 403 if not same-org/admin (identical to the
  status path).
- Query `task_events WHERE task_id = ? ORDER BY id ASC`; return the JSON array.

`handleTaskStatus` already extracts `taskID` via `filepath.Base`; add suffix
handling at the top so `/tasks/{id}/events` doesn't get misread as a task id of
`events`.

## Data flow

```
RunTask (per phase): time it → emit(TaskEvent{step,phase,outcome,detail,dur, caller})
  → Engine.EventCallback (set by launchTask) stamps TaskID+OrgID → INSERT task_events

GET /tasks/{id}/events:
  claims from context → load TaskState → authorizeTask(claims,task)
  → SELECT task_events WHERE task_id=? ORDER BY id → JSON array
```

## Error handling

- `EventCallback` nil (e.g. direct engine unit tests without wiring) → `emit` is a
  no-op; the loop is unaffected.
- `s.db.Create(&ev)` errors are non-fatal (best-effort telemetry); they do not
  abort the task. (Observability must never break execution.)
- Mock provider → tokens `0`, cost from the nominal fallback (`emit` reads
  `LastCostUSD` which the mock lacks → 0; acceptable, mock is offline).
- Unauthorized/forbidden events access → same 401/403 as `handleTaskStatus`.

## Testing

- `TokenReporter`: `recordCost` sets input/output; `LastUsage()` returns them;
  compile-time `var _ provider.TokenReporter = (*AnthropicProvider)(nil)`.
- Engine emission: run `RunTask` with mock provider/critic against a genuinely
  failing temp file + a recording `EventCallback`; assert the ordered phases
  (`initial_test:fail`, `actor:proposed`, `critic:approved`, `test:pass`) with
  sane fields (non-negative duration, correct step numbers).
- Events endpoint: seed `task_events` for a task in a temp DB (with `OrgID`), call
  `GET /tasks/{id}/events` via httptest with matching claims → JSON array; with a
  different org's claims → 403; unknown task → 404. (Reuse the auth test helpers
  from `server_idempotency_test.go` / `idempotency_test.go`.)

## Build / verification

- `CGO_ENABLED=0 go test ./pkg/...` green; `gofmt`/`go vet` clean (CI enforces).
- Build both binaries (`./cmd/...`, external linkmode + codesign).
- Manual: run a mock task end-to-end, then `GET /tasks/{id}/events` and confirm the
  per-phase timeline (initial_test → actor → critic → test) with durations and
  outcomes.

## Risks / notes

- Emission is best-effort and additive; it cannot change loop behavior or fail a
  task. Existing `e.log` output is retained so no current consumer breaks.
- One row per phase per iteration; bounded by `MaxSteps` (default 5) × ~3 phases —
  small. No pagination needed at this scale (noted for the future).
- Detail text is truncated to keep rows small and avoid duplicating full logs
  (the `logs` column still holds the complete transcript).
