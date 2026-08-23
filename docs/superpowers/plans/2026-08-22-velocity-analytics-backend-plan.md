# Engineering Velocity Analytics Backend Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Report zero-shot vs. self-healed vs. human-guided pass rates for an org's recent jobs, sourced from data that's already durably recorded.

**Architecture:** Every completed job already gets a signed `ver.Record` (`pkg/store/ver.go`, `ee/orchestrator/ver_hook.go`), and that record already carries the exact signal this needs: `WorkerAttestation.CriticRejections` (`pkg/ver/record.go:88`, how many times the Architect sent a round back for revision) and `Verification.FinalOutcome` (`pkg/ver/record.go:109`, `"pass"`/`"fail"`). Zero critic rejections plus a pass is zero-shot; one or more rejections plus a pass is self-healed. "Human-guided" is a separate, already-recorded fact: a job whose task thread contains a task with `Origin == store.OriginPRComment` (`pkg/store/lineage.go:25`) had a human intervene after the fact. No new event stream, no new instrumentation for pass-rate — only a new store query and one endpoint.

**Tech Stack:** Go 1.25, GORM/PostgreSQL.

**Spec:** `docs/superpowers/specs/2026-08-22-platform-overhaul-backend-spec.md` (read §0 Reconciliation first)

## Global Constraints

- `gofmt -l cmd/ pkg/ ee/` must print nothing; `go test ./pkg/...` and `go test ./ee/...` must pass before every commit.
- No new migration or table.
- **Cross-plan dependency — read before starting:** `ver_hook.go`'s record-assembly trigger (`ee/orchestrator/ver_hook.go`, the loop `for _, t := range tasks { if t.Status != store.TaskSucceeded && t.Status != store.TaskFailed { return } }`) only assembles a record once *every* task on a job has reached `SUCCEEDED` or `FAILED`. If the plan-mode-and-routing plan (`docs/superpowers/plans/2026-08-22-plan-mode-and-routing-backend-plan.md`) has landed, a job that went through Plan Mode has one task permanently at `PLAN_REVIEW` (a third terminal status that loop doesn't check for) — so **no execution record is ever assembled for a plan-mode job**, and this plan's velocity numbers silently exclude every one of them. This plan does not fix that gate (it belongs to Plan A's blast radius, not this one) — but Task 2's test must include a plan-mode job in the fixture and assert it's correctly excluded rather than crashing or double-counting, so the gap is visible in the test suite rather than discovered in production. If you're executing plans in order and Plan A already landed, flag this gap back rather than silently patching `ver_hook.go` as a side effect of this plan.
- **Known gap, not built here:** spec §3 Group C's `pipeline_stage_latencies` (`clone_and_provision_sec`, `env_prep_sec`, etc.) has no data source. The signed `ver.Record`'s `WorkerStep` (`pkg/ver/record.go:96-105`) deliberately carries no duration — only `Step`/`Phase`/`Outcome`/hashed detail, because an attestation commits to hashes for determinism, not raw timing. The raw wire type `ver.TaskEvent` *does* carry `DurationMs` (`pkg/ver/assemble.go:26`), but it's dropped when `AssembleRecord` converts events into `WorkerStep`s. Two ways to close this, deliberately not chosen here without a decision from whoever owns `pkg/ver`'s schema: (a) add `DurationMs` to `WorkerStep` — a schema change to a signed attestation format, which needs sign-off since old records won't have it; (b) store per-event durations in a separate, unsigned telemetry table keyed by task, populated alongside `ver_hook.go`'s existing record assembly. Do not build either as a side effect of this plan; this plan reports `test_pass_metrics` and `plan_acceptance_metrics` only, and the endpoint's response simply omits `pipeline_stage_latencies` rather than fabricating it.

---

## Task 1: `ListExecutionRecordsByOrgAndVer` store method

**Files:**
- Modify: `pkg/store/store.go` (`Store` interface, near `GetJobExecutionRecords` line 129)
- Modify: `pkg/store/ver.go` (implementation, near `GetJobExecutionRecords` line 183)
- Test: `pkg/store/ver_org_query_test.go`

**Interfaces:**
- Produces: `Store.ListExecutionRecordsByOrgAndVer(ctx, orgID, ver string, since time.Time) ([]ExecutionRecord, error)` — consumed by Task 2.

Every existing `ExecutionRecord` accessor is per-job (`GetJobExecutionRecords`, `GetExecutionRecord`); velocity analytics needs every one of an org's records in a date range, of one kind (`ver.SchemaVersion`, excluding the merge and post-merge-verify record kinds the same table also holds, distinguished by the `Ver` column per the unique index comment at `pkg/store/ver.go:20-22`).

- [ ] **Step 1: Write a failing test**

```go
package store

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestListExecutionRecordsByOrgAndVer(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	mkRecord := func(jobID, kind string) {
		_, err := s.AppendExecutionRecord(ctx, "org-1", jobID, kind, func(prevHash string) (*ExecutionRecord, error) {
			body, _ := json.Marshal(map[string]string{"job_id": jobID})
			return &ExecutionRecord{
				RecordID: jobID + "-" + kind, OrgID: "org-1", JobID: jobID, Ver: kind,
				PrevRecordHash: prevHash, RecordHash: "h-" + jobID + kind, Body: body,
			}, nil
		})
		require.NoError(t, err)
	}
	mkRecord("job-exec-1", "kiwi.ver/v1")
	mkRecord("job-exec-1", "kiwi.ver/merge/v1") // same job, different kind — must be excluded
	mkRecord("job-exec-2", "kiwi.ver/v1")

	recs, err := s.ListExecutionRecordsByOrgAndVer(ctx, "org-1", "kiwi.ver/v1", time.Now().Add(-time.Hour))
	require.NoError(t, err)
	require.Len(t, recs, 2)
	for _, r := range recs {
		require.Equal(t, "kiwi.ver/v1", r.Ver)
	}
}
```

(Use the real `ver.SchemaVersion`/`ver.MergeSchemaVersion` string constants from `pkg/ver` rather than the literal strings above if this test lives where it can import `pkg/ver` without a cycle — check first; `pkg/store` importing `pkg/ver` may or may not already happen elsewhere in this package.)

- [ ] **Step 2: Run, confirm it fails**

Run: `go test ./pkg/store/... -run TestListExecutionRecordsByOrgAndVer -v`
Expected: FAIL — `s.ListExecutionRecordsByOrgAndVer undefined`

- [ ] **Step 3: Implement**

`pkg/store/store.go`, near line 129:

```go
	// ListExecutionRecordsByOrgAndVer returns every record of one kind for an
	// org since a given time — the org-wide counterpart to GetJobExecutionRecords,
	// used for cross-job aggregation (velocity analytics).
	ListExecutionRecordsByOrgAndVer(ctx context.Context, orgID, ver string, since time.Time) ([]ExecutionRecord, error)
```

`pkg/store/ver.go`, near `GetJobExecutionRecords` (line 183):

```go
func (s *PostgresStore) ListExecutionRecordsByOrgAndVer(ctx context.Context, orgID, ver string, since time.Time) ([]ExecutionRecord, error) {
	var recs []ExecutionRecord
	err := s.db.WithContext(ctx).
		Where("org_id = ? AND ver = ? AND created_at >= ?", orgID, ver, since).
		Order("created_at desc").
		Find(&recs).Error
	return recs, err
}
```

- [ ] **Step 4: Run the test, confirm it passes; run the full store suite**

Run: `go test ./pkg/store/... -v`
Expected: PASS

- [ ] **Step 5: `gofmt -w`, commit**

```bash
gofmt -w pkg/store/
git add pkg/store/store.go pkg/store/ver.go pkg/store/ver_org_query_test.go
git commit -m "feat(store): add org-wide execution record listing for analytics"
```

---

## Task 2: `GET /api/v1/analytics/velocity`

**Files:**
- Create: `ee/orchestrator/velocity_analytics_api.go`
- Modify: `ee/orchestrator/server.go` (register the route, near `/api/v1/analytics/caching` if the prompt-cache-analytics plan already landed, otherwise near `/api/v1/spend`)
- Test: `ee/orchestrator/velocity_analytics_api_test.go`

**Interfaces:**
- Consumes: `Store.ListExecutionRecordsByOrgAndVer` (Task 1); `pkg/ver.Record` (unmarshal `ExecutionRecord.Body`); `Store.ThreadTasks(ctx, orgID, rootTaskID)` (existing, `pkg/store/store.go:73`).

- [ ] **Step 1: Write a failing test covering all three classifications plus the plan-review exclusion**

```go
func TestHandleVelocityAnalytics(t *testing.T) {
	s, mux := newTestServer(t)

	seedExecutionRecord(t, s, "org-1", "job-zero-shot", ver.Record{
		Execution: ver.Execution{Workers: []ver.WorkerAttestation{{CriticRejections: 0}}},
		Verification: ver.Verification{FinalOutcome: "pass"},
	})
	seedExecutionRecord(t, s, "org-1", "job-self-healed", ver.Record{
		Execution: ver.Execution{Workers: []ver.WorkerAttestation{{CriticRejections: 2}}},
		Verification: ver.Verification{FinalOutcome: "pass"},
	})
	// A job with a pr_comment continuation in its thread — human-guided,
	// counted separately from the pass/fail classification above.
	seedJobWithPRCommentContinuation(t, s, "org-1", "job-human-guided")
	seedExecutionRecord(t, s, "org-1", "job-human-guided", ver.Record{
		Execution: ver.Execution{Workers: []ver.WorkerAttestation{{CriticRejections: 0}}},
		Verification: ver.Verification{FinalOutcome: "pass"},
	})
	// A plan-mode job with no execution record at all (ver_hook.go's gate,
	// per this plan's Global Constraints) — must not appear in any bucket
	// and must not error the request.
	seedPlanReviewJobWithNoExecutionRecord(t, s, "org-1", "job-plan-paused")

	req := authedRequest(t, http.MethodGet, "/api/v1/analytics/velocity?range=7d", nil, "org-1")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var resp struct {
		TestPassMetrics struct {
			ZeroShotPct    float64 `json:"zero_shot_pct"`
			SelfHealedPct  float64 `json:"self_healed_pct"`
			HumanGuidedPct float64 `json:"human_guided_pct"`
		} `json:"test_pass_metrics"`
		JobsCounted int `json:"jobs_counted"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Equal(t, 3, resp.JobsCounted) // the paused job is excluded, not zero-filled
	require.InDelta(t, 33.33, resp.TestPassMetrics.ZeroShotPct, 0.1)
	require.InDelta(t, 33.33, resp.TestPassMetrics.SelfHealedPct, 0.1)
	require.InDelta(t, 33.33, resp.TestPassMetrics.HumanGuidedPct, 0.1)
}
```

Write `seedExecutionRecord` (marshals a `ver.Record` into `ExecutionRecord.Body` via `s.AppendExecutionRecord`), `seedJobWithPRCommentContinuation` (a job whose `ThreadTasks` includes a task with `Origin: store.OriginPRComment`), and `seedPlanReviewJobWithNoExecutionRecord` (a job with a `TaskPlanReview`-status task and, deliberately, no call to `AppendExecutionRecord` — matching what `ver_hook.go` actually does) as local helpers in this test file.

- [ ] **Step 2: Run, confirm it fails (404)**

Run: `go test ./ee/orchestrator/... -run TestHandleVelocityAnalytics -v`

- [ ] **Step 3: Implement**

Classification precedence matters: a job is "human-guided" if its thread has a `pr_comment` continuation, **regardless of** critic-rejection count — a human already had to step in, so it doesn't also count as zero-shot or self-healed. Check human-guided first.

```go
// SPDX-License-Identifier: LicenseRef-Kiwi-BSL-1.1
// Copyright (c) 2026 RunKiwi. Licensed under the Business Source License 1.1.
// See ee/LICENSE. This is Control Plane code and is NOT Apache-2.0.

package orchestrator

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/ibreakthecloud/kiwi/ee/auth"
	"github.com/ibreakthecloud/kiwi/pkg/store"
	"github.com/ibreakthecloud/kiwi/pkg/ver"
)

// velocityRangeDefault matches the spend endpoint's default window
// (ee/orchestrator/spend_api.go's implicit 30-day default via its `to`/`from`
// parsing) loosely, but this endpoint's spec example uses "range=7d" — accept
// a small fixed set rather than an arbitrary duration string, since this is a
// dashboard chart control, not a free-form query.
var velocityRanges = map[string]time.Duration{
	"24h": 24 * time.Hour,
	"7d":  7 * 24 * time.Hour,
	"30d": 30 * 24 * time.Hour,
}

// handleVelocityAnalytics serves GET /api/v1/analytics/velocity?range=7d.
// Classification is sourced entirely from the signed execution record
// (pkg/ver.Record) already assembled for every completed job — see this
// plan's header for why CriticRejections and FinalOutcome are sufficient,
// and its Global Constraints for what's deliberately not reported
// (pipeline_stage_latencies has no data source yet) and what's silently
// excluded (jobs paused in Plan Mode never get a record at all).
func (s *Server) handleVelocityAnalytics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	claims := auth.ClaimsFromContext(r.Context())
	if claims == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	rangeParam := r.URL.Query().Get("range")
	window, ok := velocityRanges[rangeParam]
	if !ok {
		window = velocityRanges["7d"]
	}
	since := time.Now().Add(-window)

	recs, err := s.storage.ListExecutionRecordsByOrgAndVer(r.Context(), claims.OrgID, ver.SchemaVersion, since)
	if err != nil {
		http.Error(w, "failed to load execution records", http.StatusInternalServerError)
		return
	}

	var zeroShot, selfHealed, humanGuided int
	for _, rec := range recs {
		var body ver.Record
		if err := json.Unmarshal(rec.Body, &body); err != nil {
			continue // a malformed record contributes nothing rather than skewing a bucket
		}
		if body.Verification.FinalOutcome != "pass" {
			continue // this metric is about how a pass was reached, not failure rate
		}

		humanGuidedThread, herr := s.jobThreadHasHumanContinuation(r.Context(), claims.OrgID, rec.JobID)
		if herr != nil {
			continue
		}
		switch {
		case humanGuidedThread:
			humanGuided++
		default:
			rejections := 0
			for _, worker := range body.Execution.Workers {
				rejections += worker.CriticRejections
			}
			if rejections == 0 {
				zeroShot++
			} else {
				selfHealed++
			}
		}
	}

	total := zeroShot + selfHealed + humanGuided
	pct := func(n int) float64 {
		if total == 0 {
			return 0
		}
		return float64(n) / float64(total) * 100
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"test_pass_metrics": map[string]interface{}{
			"zero_shot_pct":    pct(zeroShot),
			"self_healed_pct":  pct(selfHealed),
			"human_guided_pct": pct(humanGuided),
		},
		"jobs_counted": total,
	})
}

// jobThreadHasHumanContinuation reports whether any task in a job's thread
// (there is normally one root task per job's session; a job with multiple
// worker tasks is walked per-thread via each task's RootTaskID) has
// Origin == store.OriginPRComment — a human commented after the fact and a
// continuation ran because of it.
func (s *Server) jobThreadHasHumanContinuation(ctx context.Context, orgID, jobID string) (bool, error) {
	tasks, err := s.storage.GetJobTasks(ctx, orgID, jobID)
	if err != nil {
		return false, err
	}
	seenRoots := map[string]bool{}
	for _, t := range tasks {
		if t.RootTaskID == "" || seenRoots[t.RootTaskID] {
			continue
		}
		seenRoots[t.RootTaskID] = true
		thread, terr := s.storage.ThreadTasks(ctx, orgID, t.RootTaskID)
		if terr != nil {
			return false, terr
		}
		for _, th := range thread {
			if th.Origin == store.OriginPRComment {
				return true, nil
			}
		}
	}
	return false, nil
}
```

Register in `ee/orchestrator/server.go`:

```go
	mux.HandleFunc("/api/v1/analytics/velocity", s.handleVelocityAnalytics)
```

- [ ] **Step 4: Run the test, confirm it passes**

Run: `go test ./ee/orchestrator/... -run TestHandleVelocityAnalytics -v`
Expected: PASS

- [ ] **Step 5: Run the full orchestrator suite**

Run: `go test ./ee/orchestrator/... -v`
Expected: PASS

- [ ] **Step 6: `gofmt -w`, commit**

```bash
gofmt -w ee/orchestrator/
git add ee/orchestrator/velocity_analytics_api.go ee/orchestrator/velocity_analytics_api_test.go ee/orchestrator/server.go
git commit -m "feat(orchestrator): add zero-shot/self-healed/human-guided velocity analytics"
```

---

## Verification & Handoff Checklist

1. [ ] `gofmt -l cmd/ pkg/ ee/` returns 0 modified files.
2. [ ] `go test ./pkg/...` and `go test ./ee/...` pass 100%.
3. [ ] `go build ./...` succeeds.
4. [ ] `GET /api/v1/analytics/velocity`'s response contains no `pipeline_stage_latencies` or `plan_acceptance_metrics` field — confirm the handler doesn't emit either as a fabricated/zero placeholder; both are documented gaps (this plan's Global Constraints), not silent omissions a reader could mistake for "measured zero."
5. [ ] A job paused in Plan Mode (if Plan A has landed) is confirmed, by test, to be excluded from `jobs_counted` rather than causing an error or a miscount — this is the one correctness risk this plan inherits from a different plan's schema.
