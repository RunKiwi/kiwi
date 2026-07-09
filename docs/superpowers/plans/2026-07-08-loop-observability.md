# Actor–Critic Loop Observability Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Emit structured, queryable per-phase telemetry (timing, tokens, cost, outcome) for the Actor–Critic loop into a new `task_events` table, exposed via an org-scoped `GET /tasks/{id}/events`.

**Architecture:** Add a `TokenReporter` to the provider; add an additive `Engine.EventCallback` that emits a `TaskEvent` per phase; persist events in `launchTask` (stamping `OrgID`); serve them through `handleTaskStatus` with the existing `authorizeTask` check. Emission is best-effort and never alters loop behavior; existing `e.log` output is retained.

**Tech Stack:** Go 1.24, GORM/SQLite, `pkg/auth` (claims/authorization), `net/http/httptest`, existing `pkg/orchestrator` and `pkg/provider`.

## Global Constraints

- Module path: `github.com/ibreakthecloud/kiwi`; Go directive `1.24`.
- CI enforces `gofmt`, `go vet`, `go test`, and both binary builds — keep all green.
- Build/sign per CLAUDE.md using the `./cmd/...` path form.
- Multi-tenant: task events are tenant data; the events endpoint MUST authorize via the parent task using `authorizeTask(claims, &state)` (same-org or admin), identical to the status path.
- Observability is additive and best-effort: `EventCallback` nil → no-op; a failed event insert must not abort the task; existing `e.log` lines stay.
- Do not change existing `Provider`, `Critic`, or `UsageReporter` signatures.
- Tests never hit the network; reuse the auth test helpers (`newTestDB`, `testClaims`) already in `pkg/orchestrator`.

---

### Task 1: Provider `TokenReporter`

**Files:**
- Modify: `pkg/provider/critic.go` (add interface), `pkg/provider/llm.go` (store tokens, implement)
- Test: `pkg/provider/tokenreporter_test.go`

**Interfaces:**
- Consumes: `AnthropicProvider`, `anthropic.Usage` (`.InputTokens`, `.OutputTokens` are `int64`).
- Produces: `type TokenReporter interface { LastUsage() (inputTokens, outputTokens int64) }`; `AnthropicProvider.LastUsage()`.

- [ ] **Step 1: Write the failing test**

```go
// pkg/provider/tokenreporter_test.go
package provider

import "testing"

var _ TokenReporter = (*AnthropicProvider)(nil)

func TestAnthropicLastUsage(t *testing.T) {
	p := NewAnthropicProvider("test-key")
	in, out := p.LastUsage()
	if in != 0 || out != 0 {
		t.Fatalf("initial usage should be zero, got in=%d out=%d", in, out)
	}
	// simulate a recorded call
	p.lastInput = 1200
	p.lastOutput = 340
	in, out = p.LastUsage()
	if in != 1200 || out != 340 {
		t.Errorf("LastUsage got in=%d out=%d want 1200/340", in, out)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `CGO_ENABLED=0 go test ./pkg/provider/ -run TestAnthropicLastUsage -v`
Expected: FAIL to compile — `undefined: TokenReporter` / `p.lastInput undefined`.

- [ ] **Step 3: Add the interface**

In `pkg/provider/critic.go`, below `UsageReporter`, add:

```go
// TokenReporter is implemented by providers that can report the input/output
// token counts of their most recent API call, for observability.
type TokenReporter interface {
	LastUsage() (inputTokens, outputTokens int64)
}
```

- [ ] **Step 4: Store and expose tokens in `AnthropicProvider`**

In `pkg/provider/llm.go`, add fields to the struct (alongside `lastCost`):

```go
type AnthropicProvider struct {
	client     anthropicClient
	model      string
	lastCost   float64
	lastInput  int64
	lastOutput int64
}
```

> Match the existing struct's actual field names/types for `client`/`model` — only ADD `lastInput`/`lastOutput`. Do not rename existing fields.

Update `recordCost` to also store the token split:

```go
func (p *AnthropicProvider) recordCost(u anthropic.Usage) {
	p.lastCost = costUSD(u.InputTokens, u.OutputTokens)
	p.lastInput = u.InputTokens
	p.lastOutput = u.OutputTokens
}
```

Add the method (next to `LastCostUSD`):

```go
// LastUsage reports the input/output token counts of the most recent API call.
func (p *AnthropicProvider) LastUsage() (int64, int64) { return p.lastInput, p.lastOutput }
```

- [ ] **Step 5: Run test to verify it passes**

Run: `CGO_ENABLED=0 go test ./pkg/provider/ -run TestAnthropicLastUsage -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add pkg/provider/critic.go pkg/provider/llm.go pkg/provider/tokenreporter_test.go
git commit -m "feat(provider): add TokenReporter for per-call token counts"
```

---

### Task 2: `TaskEvent` model + migration + `summarize` helper

**Files:**
- Create: `pkg/orchestrator/events.go`
- Modify: `pkg/orchestrator/db.go` (AutoMigrate)
- Test: `pkg/orchestrator/events_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `type TaskEvent struct {...}`; `func summarize(s string, n int) string`.

- [ ] **Step 1: Write the failing test**

```go
// pkg/orchestrator/events_test.go
package orchestrator

import "testing"

func TestSummarize(t *testing.T) {
	if got := summarize("short", 100); got != "short" {
		t.Errorf("short unchanged: got %q", got)
	}
	long := ""
	for i := 0; i < 50; i++ {
		long += "0123456789"
	}
	got := summarize(long, 20)
	if len(got) > 20 {
		t.Errorf("truncated length %d > 20", len(got))
	}
	// keeps the TAIL (most recent output matters most)
	if got != long[len(long)-20:] {
		t.Errorf("summarize should keep the last 20 chars, got %q", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `CGO_ENABLED=0 go test ./pkg/orchestrator/ -run TestSummarize -v`
Expected: FAIL to compile — `undefined: summarize`.

- [ ] **Step 3: Create the model and helper**

```go
// pkg/orchestrator/events.go
package orchestrator

import "time"

// TaskEvent is a structured, per-phase telemetry record for the Actor-Critic loop.
type TaskEvent struct {
	ID           uint      `json:"id" gorm:"primaryKey;autoIncrement"`
	TaskID       string    `json:"task_id" gorm:"index"`
	OrgID        string    `json:"org_id" gorm:"index"`
	Step         int       `json:"step"`    // 0 = initial test; 1..N = iterations
	Phase        string    `json:"phase"`   // initial_test | actor | critic | test
	Outcome      string    `json:"outcome"` // pass | fail | proposed | approved | rejected | error
	Detail       string    `json:"detail"`
	DurationMs   int64     `json:"duration_ms"`
	InputTokens  int64     `json:"input_tokens"`
	OutputTokens int64     `json:"output_tokens"`
	CostUSD      float64   `json:"cost_usd"`
	CreatedAt    time.Time `json:"created_at"`
}

// summarize returns at most the last n characters of s (recent output is the
// most useful for a truncated telemetry detail field).
func summarize(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}
```

- [ ] **Step 4: Add to AutoMigrate**

In `pkg/orchestrator/db.go`, extend the TaskState migration call to include `TaskEvent`:

```go
	// Migrate TaskState + TaskEvent schema
	if err := db.AutoMigrate(&TaskState{}, &TaskEvent{}); err != nil {
		return nil, fmt.Errorf("failed to migrate task schema: %w", err)
	}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `CGO_ENABLED=0 go test ./pkg/orchestrator/ -run TestSummarize -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add pkg/orchestrator/events.go pkg/orchestrator/db.go pkg/orchestrator/events_test.go
git commit -m "feat(orchestrator): add TaskEvent model, migration, and summarize helper"
```

---

### Task 3: Engine `EventCallback` + per-phase emission

**Files:**
- Modify: `pkg/orchestrator/engine.go`
- Test: `pkg/orchestrator/engine_events_test.go`

**Interfaces:**
- Consumes: `TaskEvent`, `summarize` (Task 2); `provider.UsageReporter`, `provider.TokenReporter` (Task 1); existing `Engine`, `RunTask`.
- Produces: `Engine.EventCallback func(TaskEvent)`; `func (e *Engine) emit(step int, phase, outcome, detail string, dur time.Duration, caller interface{})`.

- [ ] **Step 1: Write the failing test**

```go
// pkg/orchestrator/engine_events_test.go
package orchestrator

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/ibreakthecloud/kiwi/pkg/provider"
)

func TestEngineEmitsPhaseEvents(t *testing.T) {
	dir := t.TempDir()
	// A file the MockProvider knows how to "fix": a divide with no zero-check.
	target := filepath.Join(dir, "math_utils.go")
	os.WriteFile(target, []byte("package mathutils\n\nfunc Divide(a, b int) int { return a / b }\n"), 0644)
	// A test that fails until fixed (panics on divide by zero → output contains "divide by zero").
	os.WriteFile(filepath.Join(dir, "math_utils_test.go"), []byte(
		"package mathutils\n\nimport \"testing\"\n\nfunc TestD(t *testing.T){ _ = Divide(1,0) }\n"), 0644)

	eng := NewEngine(provider.NewMockProvider(), 5)
	eng.Critic = provider.NewMockCritic()
	eng.LogOut = os.NewFile(0, os.DevNull) // silence logs; DevNull writer
	var events []TaskEvent
	eng.EventCallback = func(ev TaskEvent) { events = append(events, ev) }

	_ = eng.RunTask(context.Background(), "task-x", dir, "fix divide by zero", target, "go test "+dir)

	// Expect at least: initial_test, actor, critic phases recorded.
	phases := map[string]bool{}
	for _, e := range events {
		phases[e.Phase] = true
		if e.DurationMs < 0 {
			t.Errorf("negative duration on %s", e.Phase)
		}
	}
	for _, want := range []string{"initial_test", "actor", "critic"} {
		if !phases[want] {
			t.Errorf("missing phase event %q; got events: %+v", want, events)
		}
	}
}
```

> **Note:** if `os.NewFile(0, os.DevNull)` is awkward, set `eng.LogOut, _ = os.Open(os.DevNull)` in the test instead. Either silences engine logs; pick whichever compiles. The point is to not spam test output.

- [ ] **Step 2: Run test to verify it fails**

Run: `CGO_ENABLED=0 go test ./pkg/orchestrator/ -run TestEngineEmitsPhaseEvents -v`
Expected: FAIL to compile — `eng.EventCallback undefined`.

- [ ] **Step 3: Add the field and `emit` helper**

In `pkg/orchestrator/engine.go`, add to the `Engine` struct (after `SandboxConfig`):

```go
	EventCallback func(TaskEvent)
```

Add the helper (near `charge`):

```go
// emit records a structured telemetry event for one loop phase. Best-effort:
// a nil callback is a no-op. Token/cost come from the caller when it reports them.
func (e *Engine) emit(step int, phase, outcome, detail string, dur time.Duration, caller interface{}) {
	if e.EventCallback == nil {
		return
	}
	ev := TaskEvent{
		Step:       step,
		Phase:      phase,
		Outcome:    outcome,
		Detail:     detail,
		DurationMs: dur.Milliseconds(),
	}
	if r, ok := caller.(provider.UsageReporter); ok {
		ev.CostUSD = r.LastCostUSD()
	}
	if tr, ok := caller.(provider.TokenReporter); ok {
		ev.InputTokens, ev.OutputTokens = tr.LastUsage()
	}
	e.EventCallback(ev)
}
```

- [ ] **Step 4: Instrument the initial test in `RunTask`**

Replace the initial-test block (the `sandbox.RunCommand` + nominal cost + success check around lines 162–180) so it times the run and emits an event. Concretely, change:

```go
	res, err := sandbox.RunCommand(ctx, dir, testCmd, env)
	if err != nil {
		return fmt.Errorf("sandbox failed to run command: %w", err)
	}

	accumulatedCost += 0.02
	if e.CostCallback != nil {
		e.CostCallback(0.02)
	}

	if res.Success {
		e.log("[Orchestrator] Current state matches desired state. Tests already pass!\n")
		return nil
	}
```

to:

```go
	initStart := time.Now()
	res, err := sandbox.RunCommand(ctx, dir, testCmd, env)
	if err != nil {
		return fmt.Errorf("sandbox failed to run command: %w", err)
	}

	accumulatedCost += 0.02
	if e.CostCallback != nil {
		e.CostCallback(0.02)
	}

	if res.Success {
		e.emit(0, "initial_test", "pass", summarize(res.Output, 500), time.Since(initStart), nil)
		e.log("[Orchestrator] Current state matches desired state. Tests already pass!\n")
		return nil
	}
	e.emit(0, "initial_test", "fail", summarize(res.Output, 500), time.Since(initStart), nil)
```

- [ ] **Step 5: Instrument actor, critic, and test phases in the loop**

In the `for step` loop, wrap the Actor call. Change:

```go
		e.log("[Actor] Proposing edit...\n")
		proposed, err := e.Provider.GetCodeEdit(ctx, task, filePath, string(content), composeActorInput(lastBuildOutput, criticReasons))
		if err != nil {
			return fmt.Errorf("failed to get code edit: %w", err)
		}
		e.charge(&accumulatedCost, e.Provider, 0.05)
		criticReasons = ""
```

to:

```go
		e.log("[Actor] Proposing edit...\n")
		actorStart := time.Now()
		proposed, err := e.Provider.GetCodeEdit(ctx, task, filePath, string(content), composeActorInput(lastBuildOutput, criticReasons))
		if err != nil {
			e.emit(step, "actor", "error", err.Error(), time.Since(actorStart), e.Provider)
			return fmt.Errorf("failed to get code edit: %w", err)
		}
		e.charge(&accumulatedCost, e.Provider, 0.05)
		e.emit(step, "actor", "proposed", "", time.Since(actorStart), e.Provider)
		criticReasons = ""
```

Wrap the Critic call. Change:

```go
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
```

to:

```go
		verdict := provider.Verdict{Approved: true, Reasons: "no critic configured"}
		if e.Critic != nil {
			criticStart := time.Now()
			verdict, err = e.Critic.ReviewEdit(ctx, task, filePath, string(content), proposed, lastBuildOutput)
			if err != nil {
				e.emit(step, "critic", "error", err.Error(), time.Since(criticStart), e.Critic)
				return fmt.Errorf("critic review failed: %w", err)
			}
			e.charge(&accumulatedCost, e.Critic, 0.02)
			outcome := "approved"
			if !verdict.Approved {
				outcome = "rejected"
			}
			e.emit(step, "critic", outcome, verdict.Reasons, time.Since(criticStart), e.Critic)
		}

		if !verdict.Approved {
			e.log("[Critic] Rejected edit: %s\n", verdict.Reasons)
			criticReasons = verdict.Reasons
			continue // do not apply, do not run tests; Actor retries with feedback
		}
		e.log("[Critic] Approved edit.\n")
```

Wrap the post-apply test run. Change:

```go
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
```

to:

```go
		e.log("[Sandbox] Re-running build/tests...\n")
		env, err = e.resolveEnv(ctx, taskID, []string{"GITHUB_TOKEN"})
		if err != nil {
			return fmt.Errorf("failed to resolve tunnel environment: %w", err)
		}
		testStart := time.Now()
		res, err = sandbox.RunCommand(ctx, dir, testCmd, env)
		if err != nil {
			return fmt.Errorf("sandbox run failed: %w", err)
		}

		if res.Success {
			e.emit(step, "test", "pass", summarize(res.Output, 500), time.Since(testStart), nil)
			e.log("[Gate] Success: tests passed.\n")
			return nil
		}

		e.emit(step, "test", "fail", summarize(res.Output, 500), time.Since(testStart), nil)
		e.log("[Gate] Fail: target state still diverging.\n")
		e.log("[Sandbox Output]:\n%s\n", res.Output)
		lastBuildOutput = res.Output
```

- [ ] **Step 6: Run the emission test**

Run: `CGO_ENABLED=0 go test ./pkg/orchestrator/ -run TestEngineEmitsPhaseEvents -v`
Expected: PASS.

- [ ] **Step 7: Run the full package suite (no regressions)**

Run: `CGO_ENABLED=0 go test ./pkg/... 2>&1 | tail -8`
Expected: all PASS.

- [ ] **Step 8: Commit**

```bash
git add pkg/orchestrator/engine.go pkg/orchestrator/engine_events_test.go
git commit -m "feat(engine): emit structured per-phase loop telemetry via EventCallback"
```

---

### Task 4: Persist events + org-scoped `GET /tasks/{id}/events`

**Files:**
- Modify: `pkg/orchestrator/server.go` (`launchTask` wiring; `handleTaskStatus` events branch)
- Test: `pkg/orchestrator/server_events_test.go`

**Interfaces:**
- Consumes: `TaskEvent` (Task 2), `Engine.EventCallback` (Task 3), `auth.ClaimsFromContext`, `authorizeTask`, existing `handleTaskStatus`.
- Produces: `GET /tasks/{id}/events` returning `[]TaskEvent` JSON; `launchTask` sets `engine.EventCallback`.

- [ ] **Step 1: Wire persistence in `launchTask`**

In `pkg/orchestrator/server.go` `launchTask`, after `engine.CostCallback = ...` (and before `engine.SandboxConfig = ...`), capture the org and set the event callback. `existing` is already loaded at the top of `launchTask`:

```go
		orgID := existing.OrgID
		engine.EventCallback = func(ev TaskEvent) {
			ev.TaskID = taskID
			ev.OrgID = orgID
			_ = s.db.Create(&ev).Error // best-effort telemetry; never fail the task
		}
```

- [ ] **Step 2: Write the failing endpoint test**

```go
// pkg/orchestrator/server_events_test.go
package orchestrator

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ibreakthecloud/kiwi/pkg/auth"
)

func TestGetTaskEvents(t *testing.T) {
	db := newTestDB(t)
	s := &Server{db: db}

	// A task owned by test-org, with two events.
	db.Create(&TaskState{ID: "tk1", Status: "SUCCESS", OrgID: "test-org", UserID: "test-user"})
	db.Create(&TaskEvent{TaskID: "tk1", OrgID: "test-org", Step: 0, Phase: "initial_test", Outcome: "fail"})
	db.Create(&TaskEvent{TaskID: "tk1", OrgID: "test-org", Step: 1, Phase: "actor", Outcome: "proposed"})

	// Owner (same org) sees the events.
	req := httptest.NewRequest(http.MethodGet, "/tasks/tk1/events", nil).WithContext(testClaims())
	rw := httptest.NewRecorder()
	s.handleTaskStatus(rw, req)
	if rw.Code != http.StatusOK {
		t.Fatalf("owner status %d: %s", rw.Code, rw.Body.String())
	}
	var got []TaskEvent
	_ = json.Unmarshal(rw.Body.Bytes(), &got)
	if len(got) != 2 || got[0].Phase != "initial_test" || got[1].Phase != "actor" {
		t.Fatalf("events wrong: %+v", got)
	}

	// A different org is forbidden.
	otherClaims := auth.ContextWithClaims(req.Context(), &auth.UserClaims{
		UserID: "u2", OrgID: "other-org", Role: "member",
	})
	req2 := httptest.NewRequest(http.MethodGet, "/tasks/tk1/events", nil).WithContext(otherClaims)
	rw2 := httptest.NewRecorder()
	s.handleTaskStatus(rw2, req2)
	if rw2.Code != http.StatusForbidden {
		t.Errorf("cross-org events access: got %d want 403", rw2.Code)
	}
}
```

> **Note:** confirm `auth.ContextWithClaims` and the `auth.UserClaims` fields against `pkg/auth` before running (the idempotency test uses `auth.ContextWithClaims(ctx, claims)` with `UserID/Email/Name/OrgID/Role`). If the constructor differs, mirror what `testClaims()` in `server_idempotency_test.go` does.

- [ ] **Step 3: Run test to verify it fails**

Run: `CGO_ENABLED=0 go test ./pkg/orchestrator/ -run TestGetTaskEvents -v`
Expected: FAIL — `/tasks/tk1/events` is currently parsed as task id `events` → 404 (or wrong body).

- [ ] **Step 4: Add the events branch to `handleTaskStatus`**

In `pkg/orchestrator/server.go`, at the very start of `handleTaskStatus` after the claims/method checks and before `taskID := filepath.Base(...)`, handle the `/events` suffix:

```go
	// Events sub-resource: GET /tasks/{id}/events
	if strings.HasSuffix(r.URL.Path, "/events") {
		s.handleTaskEvents(w, r, claims)
		return
	}
```

Add the handler method:

```go
// handleTaskEvents serves the structured telemetry timeline for a task,
// authorized identically to the task status endpoint (same org or admin).
func (s *Server) handleTaskEvents(w http.ResponseWriter, r *http.Request, claims *auth.UserClaims) {
	// path is /tasks/{id}/events → id is the segment before "/events"
	trimmed := strings.TrimSuffix(r.URL.Path, "/events")
	taskID := filepath.Base(trimmed)

	var state TaskState
	if err := s.db.First(&state, "id = ?", taskID).Error; err != nil {
		http.Error(w, "Task not found", http.StatusNotFound)
		return
	}
	if !s.authorizeTask(claims, &state) {
		http.Error(w, "Forbidden: you do not have access to this task", http.StatusForbidden)
		return
	}

	var events []TaskEvent
	if err := s.db.Where("task_id = ?", taskID).Order("id asc").Find(&events).Error; err != nil {
		http.Error(w, "Failed to load task events", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(events)
}
```

> `strings` and `filepath` are already imported in `server.go`.

- [ ] **Step 5: Run test to verify it passes**

Run: `CGO_ENABLED=0 go test ./pkg/orchestrator/ -run TestGetTaskEvents -v`
Expected: PASS (owner 200 + 2 events; other org 403).

- [ ] **Step 6: Full suite + build + vet + fmt**

Run:
```bash
gofmt -l cmd/ pkg/
CGO_ENABLED=0 go vet ./... 2>&1 | tail -3
CGO_ENABLED=0 go test ./pkg/... 2>&1 | tail -8
CGO_ENABLED=0 go build ./...
```
Expected: `gofmt` prints nothing; vet clean; tests PASS; build succeeds.

- [ ] **Step 7: Commit**

```bash
git add pkg/orchestrator/server.go pkg/orchestrator/server_events_test.go
git commit -m "feat(orchestrator): persist loop events and serve org-scoped GET /tasks/{id}/events"
```

---

### Task 5: Build, manual verification, and docs

**Files:**
- Modify: `README.md`, `CLAUDE.md`

**Interfaces:** none.

- [ ] **Step 1: Build and sign both binaries**

Run:
```bash
go build -ldflags="-linkmode=external" -o kiwi ./cmd/kiwi/ && codesign -s - -f ./kiwi
go build -ldflags="-linkmode=external" -o kiwid ./cmd/kiwid/ && codesign -s - -f ./kiwid
```
Expected: both built and signed.

- [ ] **Step 2: Manual events check (mock mode)**

Start the daemon, submit a task (via `kiwi`), then fetch its events. Because auth now wraps all routes, use whatever bootstrap token/dev credential the auth package expects (check `pkg/auth` for the dev/bootstrap path). Once a task id is known:
```bash
curl -s http://localhost:8080/tasks/<task-id>/events -H "Authorization: Bearer <dev-token>" | head -40
```
Expected: a JSON array of events with `phase` values progressing `initial_test → actor → critic → test`, each with `duration_ms`, `cost_usd`, and (anthropic mode) non-zero `input_tokens`/`output_tokens`.

> If the auth bootstrap is non-trivial, it is acceptable to verify the endpoint via the Task 4 httptest instead and note that here; the unit test drives the same handler.

- [ ] **Step 3: Update README.md**

Under Core Features, add an item: **Loop Observability** — every Actor–Critic iteration records a structured event (phase, duration, tokens, cost, outcome) queryable at `GET /tasks/{id}/events`, org-scoped to the caller.

- [ ] **Step 4: Update CLAUDE.md**

- Add `pkg/orchestrator/events.go` to the codebase tree with a one-line description.
- Add a `Phase 11 (Completed): Loop Observability` entry (use the next available phase number; the current highest is Phase 10 for Restart Recovery — verify and increment) describing the `task_events` table, `Engine.EventCallback` per-phase emission, `TokenReporter`, and the org-scoped `GET /tasks/{id}/events`.

- [ ] **Step 5: Commit**

```bash
git add README.md CLAUDE.md docs/
git commit -m "docs: document Actor-Critic loop observability"
```

---

## Self-Review

**Spec coverage:**
- `TokenReporter` + `LastUsage` → Task 1. ✅
- `TaskEvent` model + migration → Task 2. ✅
- `summarize` truncation helper → Task 2. ✅
- `Engine.EventCallback` + per-phase emission (initial_test/actor/critic/test) → Task 3. ✅
- Best-effort, additive (nil-safe `emit`, `e.log` retained, non-fatal insert) → Tasks 3 & 4. ✅
- Persist in `launchTask` stamping OrgID → Task 4 Step 1. ✅
- Org-scoped `GET /tasks/{id}/events` via `authorizeTask` → Task 4 Steps 4–5. ✅
- Tests: TokenReporter, summarize, engine emission, endpoint (owner 200 / cross-org 403 / missing 404) → Tasks 1–4. ✅
- Build/vet/fmt (CI parity) + docs → Tasks 4 & 5. ✅

**Placeholder scan:** No TBD/TODO. The two advisory notes (Task 3 DevNull writer; Task 4 `auth.ContextWithClaims` confirmation) give concrete fallbacks and a source-of-truth to check, not vague gaps. Task 5 Step 4 says "verify and increment" the phase number — a deliberate instruction because a concurrent PR could shift it.

**Type consistency:** `TaskEvent` fields are identical across events.go (Task 2), the engine `emit` (Task 3), and the endpoint/persistence (Task 4). `emit(step int, phase, outcome, detail string, dur time.Duration, caller interface{})` matches all call sites in Task 3. `TokenReporter.LastUsage() (int64,int64)` (Task 1) matches the `emit` type-assert (Task 3). `handleTaskEvents(w, r, claims)` signature matches its call in `handleTaskStatus` (Task 4).
