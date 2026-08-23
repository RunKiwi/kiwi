# Prompt-Cache Analytics Backend Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Report how many tokens were served from prompt cache vs. raw, and the resulting dollar savings, aggregated across an org's tasks.

**Architecture:** The hard part is already built: `pkg/provider` already tracks `CacheReadTokens`/`CacheWriteTokens` per call (`pkg/provider/tools.go:69`) and `ModelCostUSDWithCache` (`pkg/provider/parse.go:149`) already prices them at their real per-model cache rate. The gap is one line: `pkg/daemon/session_store.go:115` collapses `CacheReadTokens + CacheWriteTokens` into `TokensIn` before persisting, so the split never reaches the database. This plan stops that collapse, adds a column to carry it, and adds one aggregation endpoint. Nothing about how providers report usage or how cost is computed changes.

**Tech Stack:** Go 1.25, GORM/PostgreSQL.

**Spec:** `docs/superpowers/specs/2026-08-22-platform-overhaul-backend-spec.md` (read §0 Reconciliation first)

## Global Constraints

- `gofmt -l cmd/ pkg/ ee/` must print nothing; `go test ./pkg/...` and `go test ./ee/...` must pass before every commit.
- `queued_tasks` is **not** in the numbered migrations — per `ee/orchestrator/db.go:45-48`, it's created via `db.AutoMigrate(&store.QueuedTask{}, ...)`. A new column on `QueuedTask` needs no `.up.sql` file; adding the struct field is sufficient, and `AutoMigrate` adds the column on next boot. Do not write a migration file for Task 1 — this is deliberately different from `jobs`, which does go through numbered migrations (see Plan A). If this surprises you, re-read `ee/orchestrator/db.go` before proceeding; don't silently "fix" the inconsistency by moving `queued_tasks` into numbered migrations as part of this plan — that's a separate, larger change out of scope here.
- `pkg/daemon` is Apache-2.0 and must not import `ee/`.

---

## Phase 1: Persist the Split

### Task 1: Add `CachedPromptTokens`/`RawPromptTokens` to `QueuedTask`

**Files:**
- Modify: `pkg/store/queue_models.go` (`QueuedTask` struct, currently lines 42-109)
- Test: `pkg/store/queue_cache_tokens_test.go`

**Interfaces:**
- Produces: `QueuedTask.CachedPromptTokens int64`, `QueuedTask.RawPromptTokens int64` — consumed by Task 3 (`CompleteTask`) and Task 4 (aggregation).

- [ ] **Step 1: Write a failing test**

```go
package store

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestQueuedTaskCacheTokenColumnsRoundTrip(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	task := &QueuedTask{
		ID: "t-cache-1", OrgID: "org-1", Status: TaskQueued, Spec: map[string]interface{}{},
	}
	require.NoError(t, s.EnqueueTask(ctx, task))
	leased, err := s.LeaseNextTask(ctx, "org-1", "daemon-1", "", time.Minute)
	require.NoError(t, err)

	ok, err := s.CompleteTask(ctx, TaskCompletion{
		TaskID: "t-cache-1", LeaseID: *leased.LeaseID, FinalStatus: TaskSucceeded,
		TokensIn: 1000, TokensOut: 200,
		CachedPromptTokens: 800, RawPromptTokens: 200,
	})
	require.NoError(t, err)
	require.True(t, ok)

	got, err := s.GetQueuedTask(ctx, "t-cache-1")
	require.NoError(t, err)
	require.Equal(t, int64(800), got.CachedPromptTokens)
	require.Equal(t, int64(200), got.RawPromptTokens)
}
```

- [ ] **Step 2: Run, confirm it fails**

Run: `go test ./pkg/store/... -run TestQueuedTaskCacheTokenColumnsRoundTrip -v`
Expected: FAIL — `unknown field 'CachedPromptTokens' in struct literal TaskCompletion`

- [ ] **Step 3: Add the columns and widen `TaskCompletion`**

In `pkg/store/queue_models.go`, add to `QueuedTask` after `TokensOut` (line 104):

```go
	// CachedPromptTokens and RawPromptTokens split TokensIn by whether the
	// provider served it from prompt cache. TokensIn itself stays the sum
	// (InputTokens + CacheReadTokens + CacheWriteTokens, per
	// pkg/daemon/session_store.go) so every existing reader of TokensIn is
	// unaffected; these two are additive detail for cache-discount reporting.
	CachedPromptTokens int64 `gorm:"not null;default:0" json:"cached_prompt_tokens"`
	RawPromptTokens    int64 `gorm:"not null;default:0" json:"raw_prompt_tokens"`
```

In `pkg/store/store.go`, widen `TaskCompletion` (near line 51):

```go
type TaskCompletion struct {
	TaskID, LeaseID, FinalStatus, ResultURL, Detail string
	CostUSD                                         float64
	TokensIn, TokensOut                             int64
	// CachedPromptTokens and RawPromptTokens are optional detail on top of
	// TokensIn; a caller that doesn't track the split (or an older daemon)
	// simply leaves both zero.
	CachedPromptTokens, RawPromptTokens int64
}
```

In `pkg/store/queue.go`'s `CompleteTask` (line ~335), add the two fields to the `updates` map:

```go
		updates := map[string]interface{}{
			"status":               c.FinalStatus,
			"updated_at":           now,
			"cost_usd":             c.CostUSD,
			"tokens_in":            c.TokensIn,
			"tokens_out":           c.TokensOut,
			"cached_prompt_tokens": c.CachedPromptTokens,
			"raw_prompt_tokens":    c.RawPromptTokens,
			"metered_at":           now,
		}
```

- [ ] **Step 4: Run the test, confirm it passes**

Run: `go test ./pkg/store/... -run TestQueuedTaskCacheTokenColumnsRoundTrip -v`
Expected: PASS

- [ ] **Step 5: Run the full store suite (AutoMigrate picks up the new columns automatically in tests using SQLite/Postgres test DBs)**

Run: `go test ./pkg/store/... -v`
Expected: PASS

- [ ] **Step 6: `gofmt -w pkg/store/` and commit**

```bash
git add pkg/store/queue_models.go pkg/store/store.go pkg/store/queue.go pkg/store/queue_cache_tokens_test.go
git commit -m "feat(store): persist the cached/raw prompt token split on QueuedTask"
```

---

### Task 2: Stop collapsing the split before it reaches the daemon's session store

**Files:**
- Modify: `pkg/daemon/session_store.go` (line 115, and wherever the enclosing struct is sent onward — read the function containing line 115 in full first)
- Test: `pkg/daemon/session_store_cache_test.go`

**Interfaces:**
- Consumes: `provider.ToolUsage.CacheReadTokens`/`.CacheWriteTokens` (existing, `pkg/provider/tools.go:69`).
- Produces: the value this function sends onward now also carries the split — consumed by Task 3 (wherever the daemon reports task completion; find the caller).

`pkg/daemon/session_store.go` implements `session.Store` for the durable-checkpoint channel (`pkg/session/session.go`'s `Store` field), used inside `pkg/daemon/session_run.go`'s `cpSessionStore`. Line 115's `TokensIn: total.InputTokens + total.CacheReadTokens + total.CacheWriteTokens` computes a checkpoint-progress number, not necessarily the same value that ends up in a completed task's `ResultReq`/`TaskCompletion` — read the full function around line 115 to confirm what it feeds before assuming it's the same path as `reportResult` (Plan A's Task 5 touches `reportResult` too; if both plans land, check for overlap in `pkg/daemon/daemon.go` before merging).

- [ ] **Step 1: Read `pkg/daemon/session_store.go` around line 115 and `pkg/daemon/daemon.go`'s `reportResult`/whatever function ultimately builds the final `TaskCompletion` or `ResultReq` sent to the Control Plane, and confirm where session totals (`res.Usage` from `session.Result`, or an equivalent cumulative `provider.ToolUsage`) become the `TokensIn`/`TokensOut` reported at task completion — not just at checkpoint time.**

This is investigation, no test yet — Step 2's test depends on knowing the real call chain.

- [ ] **Step 2: Write a failing test that a completed task's report carries the cache split**

The exact test depends on Step 1's finding. If task completion is reported via `pkg/daemon/daemon.go`'s `reportResult` (as Plan A's Task 5 also touches), extend that path's existing test fixture (`pkg/daemon/session_run_test.go` or `daemon_test.go`) to assert on `ResultReq.CachedPromptTokens`/`.RawPromptTokens` once those fields exist (Step 3 adds them). Sketch:

```go
func TestSessionCompletionReportsCacheTokenSplit(t *testing.T) {
	// Using this package's existing fake provider that returns a fixed
	// provider.ToolUsage with CacheReadTokens > 0 (check pkg/session's test
	// fakes, reused via pkg/daemon's session_run_test.go fixtures)...
	result := d.executeSession(ctx, spec, creds, prog, deps)
	// The ResultReq built from this (in reportResult) should carry:
	//   CachedPromptTokens == fakeUsage.CacheReadTokens + fakeUsage.CacheWriteTokens
	//   RawPromptTokens    == fakeUsage.InputTokens
	// Assert directly on whatever intermediate value reportResult reads from
	// taskResult, per Step 1's finding — this plan cannot pin the exact
	// field name until that investigation is done.
}
```

- [ ] **Step 3: Run, confirm it fails**

Run: `go test ./pkg/daemon/... -run TestSessionCompletionReportsCacheTokenSplit -v`

- [ ] **Step 4: Thread the split through**

Based on Step 1's finding, this will touch some subset of:
- `taskResult` (`pkg/daemon/daemon.go:570`): add `cachedPromptTokens, rawPromptTokens int64`.
- `reportResult` (`pkg/daemon/daemon.go:1088`): populate two new `ResultReq` fields from `out.cachedPromptTokens`/`out.rawPromptTokens`.
- `pkg/daemon/types.go`'s `ResultReq`: add
  ```go
  	// CachedPromptTokens and RawPromptTokens split TokensIn (reported
  	// separately, unchanged) by cache origin. An older daemon omits both;
  	// the Control Plane must not infer 0% cache usage from their absence.
  	CachedPromptTokens int64 `json:"cached_prompt_tokens,omitempty"`
  	RawPromptTokens    int64 `json:"raw_prompt_tokens,omitempty"`
  ```
- Wherever `session_run.go` builds `taskResult{ok: true, ...}` on success (line ~285, `return taskResult{ok: true, prURL: prURL, detail: ..., events: ...}`): add the split, computed from `res.Usage` (the cumulative `provider.ToolUsage` already on `session.Result` per `session.go:262`):
  ```go
  	return taskResult{
  		ok: true, prURL: prURL, detail: truncateDetail(detail), events: prog.all(),
  		cachedPromptTokens: res.Usage.CacheReadTokens + res.Usage.CacheWriteTokens,
  		rawPromptTokens:    res.Usage.InputTokens,
  	}
  ```

Do **not** change `session_store.go:115`'s `TokensIn` computation — that field is the checkpoint-progress total and other code may already depend on it summing all three; leave it exactly as is and add the split as new, separate reporting alongside it, at the task-completion boundary in `daemon.go`/`session_run.go` instead. (This supersedes this task's title, which assumed the collapse happened at the checkpoint boundary; Step 1 may show the real fix point is here instead — trust what Step 1 finds over the task's original framing.)

- [ ] **Step 5: Run the test, confirm it passes**

Run: `go test ./pkg/daemon/... -run TestSessionCompletionReportsCacheTokenSplit -v`

- [ ] **Step 6: Run the full daemon suite, `gofmt -w`, commit**

```bash
go test ./pkg/daemon/... -v
gofmt -w pkg/daemon/
git add pkg/daemon/daemon.go pkg/daemon/session_run.go pkg/daemon/types.go pkg/daemon/session_store_cache_test.go
git commit -m "feat(daemon): report the cached/raw prompt token split at task completion"
```

---

### Task 3: Forward the split from `handleDaemonResult` into `CompleteTask`

**Files:**
- Modify: `ee/orchestrator/daemon_api.go` (wherever `CompleteTask`/`TaskCompletion` is currently built from `ResultReq` — find with `grep -n "TaskCompletion{" ee/orchestrator/daemon_api.go`)
- Test: `ee/orchestrator/daemon_api_cache_tokens_test.go`

**Interfaces:**
- Consumes: `ResultReq.CachedPromptTokens`/`.RawPromptTokens` (Task 2); `TaskCompletion.CachedPromptTokens`/`.RawPromptTokens` (Task 1).

- [ ] **Step 1: Write a failing test**

```go
func TestHandleDaemonResultForwardsCacheTokenSplit(t *testing.T) {
	// ... existing scaffolding for posting a signed ResultReq (see Plan A's
	// Task 6 for the pattern, if it has landed; otherwise mirror an existing
	// handleDaemonResult test in this file directly)
	req := daemon.ResultReq{
		TaskID: taskID, LeaseID: leaseID, Status: "SUCCEEDED",
		SignPubKey: signPubKeyB64, ResultURL: "https://github.com/x/y/pull/1",
		CachedPromptTokens: 800, RawPromptTokens: 200,
	}
	postSignedResult(t, mux, req, signPrivKey)

	task, err := testStore.GetQueuedTask(context.Background(), taskID)
	require.NoError(t, err)
	require.Equal(t, int64(800), task.CachedPromptTokens)
}
```

- [ ] **Step 2: Run, confirm it fails**

Run: `go test ./ee/orchestrator/... -run TestHandleDaemonResultForwardsCacheTokenSplit -v`

- [ ] **Step 3: Add the two fields to the existing `TaskCompletion{...}` construction**

```go
	// existing construction, extended:
	completion := store.TaskCompletion{
		TaskID: req.TaskID, LeaseID: req.LeaseID, FinalStatus: req.Status,
		ResultURL: req.ResultURL, Detail: req.Detail,
		CostUSD: /* existing */, TokensIn: /* existing */, TokensOut: /* existing */,
		CachedPromptTokens: req.CachedPromptTokens, RawPromptTokens: req.RawPromptTokens,
	}
```

Match this exactly against whatever the existing literal actually assigns for `CostUSD`/`TokensIn`/`TokensOut` — do not guess those field sources; read the real construction before editing it.

- [ ] **Step 4: Run the test, confirm it passes; run the full orchestrator suite**

Run: `go test ./ee/orchestrator/... -v`
Expected: PASS

- [ ] **Step 5: `gofmt -w`, commit**

```bash
gofmt -w ee/orchestrator/
git add ee/orchestrator/daemon_api.go ee/orchestrator/daemon_api_cache_tokens_test.go
git commit -m "feat(orchestrator): forward the cache token split into CompleteTask"
```

---

## Phase 2: Aggregation Endpoint

### Task 4: `GET /api/v1/analytics/caching`

**Files:**
- Create: `ee/orchestrator/caching_analytics_api.go`
- Modify: `ee/orchestrator/server.go` (register the route)
- Test: `ee/orchestrator/caching_analytics_api_test.go`

**Interfaces:**
- Consumes: `store.QueuedTask.CachedPromptTokens`/`.RawPromptTokens` (Task 1); `provider.ModelCostUSDWithCache`/`cacheRates` (existing, `pkg/provider/parse.go:149`, and whatever unexported `cacheRates(model)` resolves — check its signature and whether it's exported before reusing it directly, or whether the discount needs to be recomputed per-model the same way `ModelCostUSDWithCache` already does internally).

- [ ] **Step 1: Write a failing test**

```go
func TestHandleCachingAnalytics(t *testing.T) {
	s, mux := newTestServer(t)
	seedTaskWithCacheTokens(t, s, "org-1", "job-1", "t-1", 800, 200, "claude-opus-4-8") // helper: EnqueueTask+lease+CompleteTask with the given split and spec["model"]

	req := authedRequest(t, http.MethodGet, "/api/v1/analytics/caching", nil, "org-1")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var resp struct {
		CachedPromptTokens   int64   `json:"cached_prompt_tokens"`
		RawPromptTokens      int64   `json:"raw_prompt_tokens"`
		TotalDollarSavingsUSD float64 `json:"total_dollar_savings_usd"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Equal(t, int64(800), resp.CachedPromptTokens)
	require.Equal(t, int64(200), resp.RawPromptTokens)
	require.Greater(t, resp.TotalDollarSavingsUSD, 0.0)
}
```

- [ ] **Step 2: Run, confirm it fails (404)**

Run: `go test ./ee/orchestrator/... -run TestHandleCachingAnalytics -v`

- [ ] **Step 3: Implement**

Read `pkg/provider/parse.go` in full around `ModelCostUSDWithCache`/`cacheRates`/`pricingFor` before writing this — the savings figure is "what the cached tokens would have cost at the model's normal input rate minus what they actually cost at the cache-read rate," which needs per-model rates, not a flat 90%. If `cacheRates` is unexported, either export it or add a small exported helper in `pkg/provider` (`func CacheDiscountUSD(model string, cachedTokens int64) float64`) rather than duplicating the rate table in `ee/orchestrator` — reuse over reimplementation.

```go
// SPDX-License-Identifier: LicenseRef-Kiwi-BSL-1.1
// Copyright (c) 2026 RunKiwi. Licensed under the Business Source License 1.1.
// See ee/LICENSE. This is Control Plane code and is NOT Apache-2.0.

package orchestrator

import (
	"net/http"
	"time"

	"github.com/ibreakthecloud/kiwi/ee/auth"
	"github.com/ibreakthecloud/kiwi/pkg/provider"
	"github.com/ibreakthecloud/kiwi/pkg/store"
)

// handleCachingAnalytics serves GET /api/v1/analytics/caching, summing the
// cache/raw prompt token split (Task 1-3) across the org's tasks in the last
// 30 days and pricing the savings per-model via pkg/provider's existing cache
// rate table — the same rates ModelCostUSDWithCache already applies when a
// task's real cost is metered, so this number matches what was actually
// charged rather than assuming a flat discount.
func (s *Server) handleCachingAnalytics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	claims := auth.ClaimsFromContext(r.Context())
	if claims == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	since := time.Now().Add(-30 * 24 * time.Hour)
	var tasks []store.QueuedTask
	if err := s.db.WithContext(r.Context()).
		Joins("JOIN jobs ON jobs.id = queued_tasks.job_id").
		Where("jobs.org_id = ? AND queued_tasks.created_at >= ?", claims.OrgID, since).
		Find(&tasks).Error; err != nil {
		http.Error(w, "failed to query tasks", http.StatusInternalServerError)
		return
	}

	var cached, raw int64
	var savings float64
	for _, t := range tasks {
		cached += t.CachedPromptTokens
		raw += t.RawPromptTokens
		model, _ := t.Spec["model"].(string)
		if model != "" && t.CachedPromptTokens > 0 {
			savings += provider.CacheDiscountUSD(model, t.CachedPromptTokens)
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"cached_prompt_tokens":     cached,
		"raw_prompt_tokens":        raw,
		"total_dollar_savings_usd": savings,
	})
}
```

Register in `ee/orchestrator/server.go` near `/api/v1/spend` (line 485):

```go
	mux.HandleFunc("/api/v1/analytics/caching", s.handleCachingAnalytics)
```

- [ ] **Step 4: Run the test, confirm it passes**

Run: `go test ./ee/orchestrator/... -run TestHandleCachingAnalytics -v`

- [ ] **Step 5: Run the full orchestrator and provider suites, `gofmt -w`, commit**

```bash
go test ./ee/orchestrator/... ./pkg/provider/... -v
gofmt -w ee/orchestrator/ pkg/provider/
git add ee/orchestrator/caching_analytics_api.go ee/orchestrator/caching_analytics_api_test.go ee/orchestrator/server.go pkg/provider/parse.go
git commit -m "feat(orchestrator): add prompt-cache savings analytics endpoint"
```

---

## Verification & Handoff Checklist

1. [ ] `gofmt -l cmd/ pkg/ ee/` returns 0 modified files.
2. [ ] `go test ./pkg/...` and `go test ./ee/...` pass 100%.
3. [ ] `go build ./...` succeeds.
4. [ ] `QueuedTask.TokensIn` (the existing field) is byte-for-byte unchanged in value for every existing caller — this plan adds a parallel split, it never redefines what `TokensIn` means. Confirm with a diff-focused re-read of `session_store.go:115` after Task 2: that line must be untouched.
5. [ ] A task from before this plan shipped (`CachedPromptTokens`/`RawPromptTokens` both 0 by column default) is excluded from `/api/v1/analytics/caching`'s savings sum, not counted as "0% cached" in a way that drags down an otherwise-accurate average — since this plan sums raw counts rather than an average, confirm that's still the right call for the frontend's actual chart, or note the gap.
