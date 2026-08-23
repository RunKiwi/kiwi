# Plan Mode & Model Routing Backend Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let a human review and approve/reject a session's Round-0 plan before any Implementer round runs, and let a job carry a per-job spend cap and (for display) its architect/worker model names.

**Architecture:** A session already writes a durable checkpoint containing the Architect's `Spec` immediately after planning and before Round 1 starts (`pkg/session/session.go:539`). Plan Mode adds a gate right there: if the task requires approval, the session stops and reports a new terminal `QueuedTask` status (`PLAN_REVIEW`) instead of running any round. The Control Plane renders the checkpointed spec into `Job.PlanMarkdown` and waits. Approval creates a **continuation** task — the same lineage mechanism already used for PR-comment continuations (`ParentTaskID`/`RootTaskID`/`Origin`, `pkg/store/lineage.go`) — whose daemon re-leases the same `SessionID` and resumes at round 1 via the *existing* `resumeFrom > 0` path (`pkg/session/session.go:475-486`), which already skips re-planning because the spec is already in the checkpoint. Rejection just marks the job failed. No new pause/wait mechanism, no polling, no change to how sessions execute once running.

**Tech Stack:** Go 1.25, GORM/PostgreSQL, existing lease-queue + checkpoint infrastructure.

**Spec:** `docs/superpowers/specs/2026-08-22-platform-overhaul-backend-spec.md` (read §0 Reconciliation first)

## Global Constraints

- `gofmt -l cmd/ pkg/ ee/` must print nothing; `go test ./pkg/...` and `go test ./ee/...` must pass before every commit.
- `pkg/session` and `pkg/daemon` are Apache-2.0 and **must not import anything under `ee/`** — `pkg/licensing_boundary_test.go` enforces this. The session/daemon side of Plan Mode is a `bool` on task input and a new outcome on `Result`; every piece of HTTP, org-scoping, and JSON rendering lives in `ee/orchestrator`.
- Every new table column on `jobs` is nullable or has a safe default. Every query in `ee/orchestrator` and `pkg/store` filters on `org_id`.
- Next migration number is `0046` (last is `0045_slack_binding_model_defaults.up.sql`).
- Non-superadmin routes gated through `authorizeOrgAccess(r, orgID)`; the plan endpoints are per-job, gated by the job's `org_id` matching `claims.OrgID` (see existing pattern in `ee/orchestrator/jobs_api.go:handleJobStatus`).
- **Downstream effect on `ver_hook.go`, discovered while planning velocity analytics separately:** `ee/orchestrator/ver_hook.go`'s execution-record assembly only fires once every task on a job reaches `TaskSucceeded` or `TaskFailed` (`for _, t := range tasks { if t.Status != store.TaskSucceeded && t.Status != store.TaskFailed { return } }`). A job with a `PLAN_REVIEW` task (Task 2 of this plan) has a task that will never become either — so **no signed execution record is ever assembled for a plan-mode job**, silently, with no error. This plan does not need to fix that for Plan Mode itself to work correctly, but before this plan is considered done, widen that loop's condition to also accept `store.TaskPlanReview` for a task that is *not* the thread's newest (i.e. one that was superseded by a `plan_approved` continuation) — or, more simply, have the loop skip `PLAN_REVIEW` tasks entirely when a later task in the same `RootTaskID` thread completed. Add this as a final task here if executing this plan standalone; if `docs/superpowers/plans/2026-08-22-velocity-analytics-backend-plan.md` is being executed too, its own Global Constraints repeat this same finding — do not let both plans "discover" and fix it independently in conflicting ways.

---

## Phase 1: Data Model

### Task 1: Add Plan Mode & routing columns to `Job`

**Files:**
- Modify: `pkg/store/models.go` (the `Job` struct, currently lines 121-143)
- Create: `migrations/0046_plan_mode_and_routing.up.sql`
- Test: `pkg/store/job_plan_test.go`

**Interfaces:**
- Produces: `Job.RequiresPlanApproval bool`, `Job.PlanStatus string`, `Job.PlanMarkdown string`, `Job.PlanAcceptedAt *time.Time`, `Job.PlanRejectedReason string`, `Job.ArchitectModel string`, `Job.WorkerModel string`, `Job.SpendCapUSD float64` — consumed by Task 3 (store methods) and Task 5 (HTTP handlers).

- [ ] **Step 1: Write a failing test asserting the new columns round-trip through GORM**

```go
package store

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestJobPlanModeColumnsRoundTrip(t *testing.T) {
	s := newTestStore(t) // existing helper used throughout pkg/store tests
	job := &Job{
		ID:                   "job-plan-test",
		OrgID:                "org-1",
		UserID:               "usr-1",
		Status:               "PENDING",
		Inputs:               map[string]interface{}{"task": "test"},
		RequiresPlanApproval: true,
		PlanStatus:           "drafting",
		ArchitectModel:       "claude-opus-4-8",
		WorkerModel:          "claude-haiku-4-5",
		SpendCapUSD:          0.75,
	}
	require.NoError(t, s.CreateJobWithOutbox(context.Background(), job, &Outbox{
		ID: 0, JobID: job.ID, Topic: "job.created", Payload: map[string]interface{}{},
	}))

	fetched, err := s.GetJob(context.Background(), "job-plan-test")
	require.NoError(t, err)
	require.True(t, fetched.RequiresPlanApproval)
	require.Equal(t, "drafting", fetched.PlanStatus)
	require.Equal(t, 0.75, fetched.SpendCapUSD)
	require.Equal(t, "claude-opus-4-8", fetched.ArchitectModel)
	require.Empty(t, fetched.PlanMarkdown) // default '' on a job that never asked for review
}
```

- [ ] **Step 2: Run it, confirm it fails to compile** (the new `Job` fields don't exist yet)

Run: `go test ./pkg/store/... -run TestJobPlanModeColumnsRoundTrip -v`
Expected: FAIL — `unknown field 'RequiresPlanApproval' in struct literal`

- [ ] **Step 3: Add the columns to the `Job` struct**

In `pkg/store/models.go`, inside `type Job struct { ... }`, add after the existing `UpdatedAt` field:

```go
	// RequiresPlanApproval gates the session's Round-0 plan behind a human
	// approve/reject before any Implementer round runs. See
	// docs/superpowers/plans/2026-08-22-plan-mode-and-routing-backend-plan.md.
	RequiresPlanApproval bool `gorm:"not null;default:false" json:"requires_plan_approval"`
	// PlanStatus is '' (plan mode off), 'drafting', 'pending_review',
	// 'approved', or 'rejected'.
	PlanStatus string `gorm:"type:varchar(32);not null;default:''" json:"plan_status,omitempty"`
	// PlanMarkdown is rendered from the Architect's Spec once Round 0
	// finishes (see Task 4), not written by the daemon directly.
	PlanMarkdown       string     `gorm:"type:text;not null;default:''" json:"plan_markdown,omitempty"`
	PlanAcceptedAt     *time.Time `json:"plan_accepted_at,omitempty"`
	PlanRejectedReason string     `gorm:"type:varchar(255);not null;default:''" json:"plan_rejected_reason,omitempty"`
	// ArchitectModel and WorkerModel are denormalized from the submit-time
	// request (ee/planner already routes them independently — see
	// pkg/daemon/session_run.go:83) purely for display on the job/plan views.
	ArchitectModel string  `gorm:"type:varchar(128);not null;default:''" json:"architect_model,omitempty"`
	WorkerModel    string  `gorm:"type:varchar(128);not null;default:''" json:"worker_model,omitempty"`
	SpendCapUSD    float64 `gorm:"not null;default:0" json:"spend_cap_usd,omitempty"`
```

- [ ] **Step 4: Write the migration**

```sql
-- migrations/0046_plan_mode_and_routing.up.sql
ALTER TABLE jobs ADD COLUMN IF NOT EXISTS requires_plan_approval BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE jobs ADD COLUMN IF NOT EXISTS plan_status VARCHAR(32) NOT NULL DEFAULT '';
ALTER TABLE jobs ADD COLUMN IF NOT EXISTS plan_markdown TEXT NOT NULL DEFAULT '';
ALTER TABLE jobs ADD COLUMN IF NOT EXISTS plan_accepted_at TIMESTAMPTZ;
ALTER TABLE jobs ADD COLUMN IF NOT EXISTS plan_rejected_reason VARCHAR(255) NOT NULL DEFAULT '';
ALTER TABLE jobs ADD COLUMN IF NOT EXISTS architect_model VARCHAR(128) NOT NULL DEFAULT '';
ALTER TABLE jobs ADD COLUMN IF NOT EXISTS worker_model VARCHAR(128) NOT NULL DEFAULT '';
ALTER TABLE jobs ADD COLUMN IF NOT EXISTS spend_cap_usd DOUBLE PRECISION NOT NULL DEFAULT 0;
```

- [ ] **Step 5: Run the test, confirm it passes**

Run: `go test ./pkg/store/... -run TestJobPlanModeColumnsRoundTrip -v`
Expected: PASS

- [ ] **Step 6: `gofmt -w pkg/store/models.go` and commit**

```bash
git add pkg/store/models.go migrations/0046_plan_mode_and_routing.up.sql pkg/store/job_plan_test.go
git commit -m "feat(store): add plan-mode and model-routing columns to Job"
```

---

### Task 2: Add `TaskPlanReview` queue status

**Files:**
- Modify: `pkg/store/queue_models.go` (status constants, currently lines 6-16)
- Modify: `pkg/store/queue.go` (`CompleteTask`, currently lines 327-330; `IsTerminal` is in `queue_models.go:18-22`)
- Test: `pkg/store/queue_plan_review_test.go`

**Interfaces:**
- Consumes: nothing new.
- Produces: `store.TaskPlanReview = "PLAN_REVIEW"`, `store.IsTerminal("PLAN_REVIEW") == true`, `CompleteTask` accepts `FinalStatus: store.TaskPlanReview` — consumed by Task 5 (`handleDaemonResult`) and Task 6 (daemon side).

- [ ] **Step 1: Write a failing test for the new status**

```go
package store

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCompleteTaskAcceptsPlanReviewStatus(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	task := &QueuedTask{ID: "t-plan-1", OrgID: "org-1", JobID: "job-1", Status: TaskQueued, Spec: map[string]interface{}{}}
	require.NoError(t, s.EnqueueTask(ctx, task))
	leased, err := s.LeaseNextTask(ctx, "org-1", "daemon-1", "", time.Minute)
	require.NoError(t, err)
	require.NotNil(t, leased)

	ok, err := s.CompleteTask(ctx, TaskCompletion{
		TaskID: "t-plan-1", LeaseID: *leased.LeaseID, FinalStatus: TaskPlanReview,
		Detail: "plan ready for review",
	})
	require.NoError(t, err)
	require.True(t, ok)
	require.True(t, IsTerminal(TaskPlanReview), "a plan-review task must never be re-leased or swept")
}
```

- [ ] **Step 2: Run it, confirm it fails**

Run: `go test ./pkg/store/... -run TestCompleteTaskAcceptsPlanReviewStatus -v`
Expected: FAIL — `undefined: TaskPlanReview`, then (once that's added) `invalid final status "PLAN_REVIEW"`

- [ ] **Step 3: Add the constant and widen `CompleteTask`'s validation**

In `pkg/store/queue_models.go`, in the `const` block (lines 6-16):

```go
const (
	TaskQueued    = "QUEUED"
	TaskLeased    = "LEASED"
	TaskSucceeded = "SUCCEEDED"
	TaskFailed    = "FAILED"
	TaskCancelled = "CANCELLED"
	// TaskPlanReview is terminal for the queue (this task's lease is released
	// and it is never re-leased or swept) but not terminal for the job: the
	// job stays open pending a human decision. A separate continuation task
	// (Origin=OriginPlanApproved) resumes the work once approved. See
	// docs/superpowers/plans/2026-08-22-plan-mode-and-routing-backend-plan.md.
	TaskPlanReview = "PLAN_REVIEW"
)
```

And in `IsTerminal` (`queue_models.go:20-22`):

```go
func IsTerminal(status string) bool {
	return status == TaskSucceeded || status == TaskFailed || status == TaskCancelled || status == TaskPlanReview
}
```

In `pkg/store/queue.go:328`:

```go
	if c.FinalStatus != TaskSucceeded && c.FinalStatus != TaskFailed && c.FinalStatus != TaskPlanReview {
		return false, fmt.Errorf("invalid final status %q (want %s, %s, or %s)", c.FinalStatus, TaskSucceeded, TaskFailed, TaskPlanReview)
	}
```

One more change in the same function: the existing block at `queue.go:380` (`if c.FinalStatus == TaskFailed { failJobAndQueuedTasks(...) }`) and the learning-row update below it key off `TaskSucceeded`/`TaskFailed` only — a `PLAN_REVIEW` completion must skip both (it is neither a success nor a failure of the job). Change the guard:

```go
			if c.FinalStatus == TaskFailed {
				failJobAndQueuedTasks(tx, t.JobID, "Sibling task failed")
			}
			if c.FinalStatus == TaskSucceeded || c.FinalStatus == TaskFailed {
				learningUpdates := map[string]interface{}{"outcome": strings.ToLower(c.FinalStatus)}
				if c.ResultURL != "" {
					learningUpdates["pr_url"] = c.ResultURL
				}
				if c.FinalStatus == TaskSucceeded && c.Detail != "" {
					learningUpdates["summary"] = gorm.Expr("COALESCE(NULLIF(summary, ''), ?)", c.Detail)
				}
				q := tx.Model(&JobLearning{}).Where("org_id = ? AND job_id = ?", t.OrgID, t.JobID)
				if c.FinalStatus == TaskSucceeded {
					q = q.Where("outcome IS NULL OR outcome <> ?", strings.ToLower(TaskFailed))
				}
				_ = q.Updates(learningUpdates)
			}
```

(This wraps the existing `learningUpdates` block in the new `if`; the cost/agent-minutes metering above it stays unconditional since a plan-review round still spent real money and time.)

- [ ] **Step 4: Run the test, confirm it passes**

Run: `go test ./pkg/store/... -run TestCompleteTaskAcceptsPlanReviewStatus -v`
Expected: PASS

- [ ] **Step 5: Run the full store suite to confirm nothing else assumed only two terminal statuses**

Run: `go test ./pkg/store/... -v`
Expected: PASS (existing `TestCompleteTaskFencing` at `queue_test.go:274` still rejects `"BOGUS"`, unaffected)

- [ ] **Step 6: Commit**

```bash
git add pkg/store/queue_models.go pkg/store/queue.go pkg/store/queue_plan_review_test.go
git commit -m "feat(store): add PLAN_REVIEW as a terminal queue status"
```

---

### Task 3: Add `OriginPlanApproved` and store methods for the plan lifecycle

**Files:**
- Modify: `pkg/store/lineage.go` (Origin constants, currently lines 24-28)
- Modify: `pkg/store/store.go` (`Store` interface)
- Modify: `pkg/store/postgres.go` (implementation)
- Test: `pkg/store/job_plan_lifecycle_test.go`

**Interfaces:**
- Consumes: `Job` columns from Task 1.
- Produces: `store.OriginPlanApproved = "plan_approved"`; `Store.SetJobPlanPendingReview(ctx, jobID, planMarkdown string) error`; `Store.ApproveJobPlan(ctx, jobID string) error`; `Store.RejectJobPlan(ctx, jobID, reason string) error`; `Store.SetJobSpendCap(ctx, orgID, jobID string, capUSD float64) error` — consumed by Task 5 (HTTP handlers).

- [ ] **Step 1: Write failing tests for each method**

```go
package store

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func newPlanTestJob(t *testing.T, s *PostgresStore, id string) {
	t.Helper()
	require.NoError(t, s.CreateJobWithOutbox(context.Background(), &Job{
		ID: id, OrgID: "org-1", UserID: "usr-1", Status: "RUNNING",
		Inputs: map[string]interface{}{"task": "test"}, RequiresPlanApproval: true,
	}, &Outbox{JobID: id, Topic: "job.created", Payload: map[string]interface{}{}}))
}

func TestSetJobPlanPendingReview(t *testing.T) {
	s := newTestStore(t)
	newPlanTestJob(t, s, "job-p1")
	require.NoError(t, s.SetJobPlanPendingReview(context.Background(), "job-p1", "# Plan\n1. Do the thing"))
	j, err := s.GetJob(context.Background(), "job-p1")
	require.NoError(t, err)
	require.Equal(t, "pending_review", j.PlanStatus)
	require.Equal(t, "# Plan\n1. Do the thing", j.PlanMarkdown)
	require.Equal(t, "PLAN_REVIEW", j.Status)
}

func TestApproveJobPlan(t *testing.T) {
	s := newTestStore(t)
	newPlanTestJob(t, s, "job-p2")
	require.NoError(t, s.SetJobPlanPendingReview(context.Background(), "job-p2", "plan text"))
	require.NoError(t, s.ApproveJobPlan(context.Background(), "job-p2"))
	j, err := s.GetJob(context.Background(), "job-p2")
	require.NoError(t, err)
	require.Equal(t, "approved", j.PlanStatus)
	require.Equal(t, "RUNNING", j.Status)
	require.NotNil(t, j.PlanAcceptedAt)
}

func TestRejectJobPlan(t *testing.T) {
	s := newTestStore(t)
	newPlanTestJob(t, s, "job-p3")
	require.NoError(t, s.SetJobPlanPendingReview(context.Background(), "job-p3", "plan text"))
	require.NoError(t, s.RejectJobPlan(context.Background(), "job-p3", "use CockroachDB leases instead"))
	j, err := s.GetJob(context.Background(), "job-p3")
	require.NoError(t, err)
	require.Equal(t, "rejected", j.PlanStatus)
	require.Equal(t, "FAILED", j.Status)
	require.Equal(t, "use CockroachDB leases instead", j.PlanRejectedReason)
}

func TestSetJobSpendCapRejectsOtherOrg(t *testing.T) {
	s := newTestStore(t)
	newPlanTestJob(t, s, "job-p4")
	err := s.SetJobSpendCap(context.Background(), "org-2", "job-p4", 1.50)
	require.Error(t, err) // job-p4 belongs to org-1
}
```

- [ ] **Step 2: Run, confirm compile failures**

Run: `go test ./pkg/store/... -run TestSetJobPlanPendingReview -v`
Expected: FAIL — `s.SetJobPlanPendingReview undefined`

- [ ] **Step 3: Add the constant and interface methods**

In `pkg/store/lineage.go`, next to `OriginPRComment`/`OriginFork` (lines 24-26):

```go
	// OriginPlanApproved marks a continuation created by approving a Plan
	// Mode review. It reuses the same task-lineage mechanism as OriginPRComment
	// (ParentTaskID/RootTaskID) so the daemon resumes the exact session that
	// was paused, rather than starting a fresh one.
	OriginPlanApproved = "plan_approved"
```

In `pkg/store/store.go`, in the `Store` interface, near `UpdateJobManifest` (line ~90):

```go
	// Plan Mode lifecycle. SetJobPlanPendingReview also sets Job.Status to
	// "PLAN_REVIEW" so it stops appearing as actively running; ApproveJobPlan
	// and RejectJobPlan resolve it. None of these touch QueuedTask — the
	// caller (ee/orchestrator) creates the continuation task separately once
	// SetJobPlanPendingReview or ApproveJobPlan is decided by the human.
	SetJobPlanPendingReview(ctx context.Context, jobID, planMarkdown string) error
	ApproveJobPlan(ctx context.Context, jobID string) error
	RejectJobPlan(ctx context.Context, jobID, reason string) error
	// SetJobSpendCap updates a job's per-job spend cap. orgID scopes the
	// update so an org-scoped caller cannot touch another org's job by ID.
	SetJobSpendCap(ctx context.Context, orgID, jobID string, capUSD float64) error
```

In `pkg/store/postgres.go`, alongside `UpdateJobStatus` (line 79):

```go
func (s *PostgresStore) SetJobPlanPendingReview(ctx context.Context, jobID, planMarkdown string) error {
	return s.db.WithContext(ctx).Model(&Job{}).Where("id = ?", jobID).Updates(map[string]interface{}{
		"plan_status":   "pending_review",
		"plan_markdown": planMarkdown,
		"status":        "PLAN_REVIEW",
		"updated_at":    time.Now(),
	}).Error
}

func (s *PostgresStore) ApproveJobPlan(ctx context.Context, jobID string) error {
	now := time.Now()
	return s.db.WithContext(ctx).Model(&Job{}).Where("id = ?", jobID).Updates(map[string]interface{}{
		"plan_status":       "approved",
		"plan_accepted_at":  &now,
		"status":            "RUNNING",
		"updated_at":        now,
	}).Error
}

func (s *PostgresStore) RejectJobPlan(ctx context.Context, jobID, reason string) error {
	return s.db.WithContext(ctx).Model(&Job{}).Where("id = ?", jobID).Updates(map[string]interface{}{
		"plan_status":          "rejected",
		"plan_rejected_reason": reason,
		"status":               "FAILED",
		"updated_at":           time.Now(),
	}).Error
}

func (s *PostgresStore) SetJobSpendCap(ctx context.Context, orgID, jobID string, capUSD float64) error {
	res := s.db.WithContext(ctx).Model(&Job{}).
		Where("id = ? AND org_id = ?", jobID, orgID).
		Update("spend_cap_usd", capUSD)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrJobNotFound // defined in pkg/store/postgres.go alongside ErrDaemonNotFound; add if missing, matching that pattern (errors.New)
	}
	return nil
}
```

Check whether `ErrJobNotFound` already exists (`grep -n "ErrJobNotFound" pkg/store/*.go`) before adding it — if it does, reuse it; if not, add `var ErrJobNotFound = errors.New("job not found")` next to `ErrDaemonNotFound`'s definition.

- [ ] **Step 4: Run the tests, confirm pass**

Run: `go test ./pkg/store/... -run 'TestSetJobPlanPendingReview|TestApproveJobPlan|TestRejectJobPlan|TestSetJobSpendCapRejectsOtherOrg' -v`
Expected: PASS

- [ ] **Step 5: `gofmt -w pkg/store/` and commit**

```bash
git add pkg/store/lineage.go pkg/store/store.go pkg/store/postgres.go pkg/store/job_plan_lifecycle_test.go
git commit -m "feat(store): add plan-mode lifecycle transitions for Job"
```

---

## Phase 2: Session Gate (pkg/session, pkg/daemon — Apache-2.0)

### Task 4: Gate Round 1 behind approval in `pkg/session`

**Files:**
- Modify: `pkg/session/session.go` (`Task` struct lines 68-84, `Result` struct lines 258-271, the Round-0 block around lines 505-540)
- Test: `pkg/session/plan_review_test.go`

**Interfaces:**
- Consumes: nothing from `ee/`.
- Produces: `Task.RequiresPlanApproval bool`; `Result.PlanPendingReview bool`; `Result.Spec Spec` (the full structured plan, so the caller can render it) — consumed by Task 6 (`pkg/daemon`).

- [ ] **Step 1: Write a failing test that a session with `RequiresPlanApproval` stops after planning**

Add to a new `pkg/session/plan_review_test.go`, following the existing fake-Architect/fake-Implementer test doubles already used elsewhere in this package's tests (check `pkg/session/session_test.go` for the exact fake constructors — reuse them, do not write new ones):

```go
package session

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRunStopsAfterPlanningWhenApprovalRequired(t *testing.T) {
	architect := &fakeArchitect{ // reuse the existing fake from session_test.go
		planSpec: Spec{Verdict: VerdictProposed, Objective: "add a test"},
	}
	runner := &Runner{
		Architect:   architect,
		Implementer: &fakeToolRunner{}, // must NOT be called
		Workspace:   &fakeWorkspace{},
		Verify:      func(ctx context.Context) (string, bool, error) { return "", true, nil },
		Config:      Config{MaxRounds: 3},
	}

	res, err := runner.Run(context.Background(), Task{
		ID: "t1", Description: "add a test", TestCmd: "true",
		RequiresPlanApproval: true,
	})

	require.NoError(t, err)
	require.True(t, res.PlanPendingReview)
	require.Equal(t, "add a test", res.Spec.Objective)
	require.Equal(t, 0, architect.reviewCalls, "review must not run before approval")
	require.Equal(t, 0, runner.Implementer.(*fakeToolRunner).callCount, "the implementer must not run before approval")
}
```

(If `fakeArchitect`/`fakeToolRunner`/`fakeWorkspace` in the existing test file don't expose `reviewCalls`/`callCount` counters, add them there rather than duplicating the fakes — same rule as the rest of this plan: reuse before you build.)

- [ ] **Step 2: Run it, confirm it fails**

Run: `go test ./pkg/session/... -run TestRunStopsAfterPlanningWhenApprovalRequired -v`
Expected: FAIL — `unknown field 'RequiresPlanApproval' in struct literal Task`

- [ ] **Step 3: Add the fields and the gate**

In `pkg/session/session.go`, add to `Task` (after `Learnings []string` at line 83):

```go
	// RequiresPlanApproval stops the session after Round 0 planning and
	// before any Implementer round, reporting PlanPendingReview instead of
	// running further. A later run of the same SessionID with this task's
	// checkpoint already at round 1 (the normal resumeFrom>0 path) proceeds
	// straight into the round loop without re-planning, because the approved
	// spec is already in that checkpoint.
	RequiresPlanApproval bool
```

Add to `Result` (after `HeadSHA string` at line 270):

```go
	// PlanPendingReview is true when the session stopped after planning
	// because Task.RequiresPlanApproval was set. Success is false in this
	// case — the task did not finish, it paused.
	PlanPendingReview bool
	// Spec is the Architect's Round-0 plan, populated only when
	// PlanPendingReview is true. The caller renders it for a human to read.
	Spec Spec
```

In the Round-0 block, right after the existing checkpoint save (`session.go:539`, `r.save(ctx, st, 1, 0)`), insert the gate before the function proceeds into `r.rounds(...)`:

```go
	r.save(ctx, st, 1, 0)

	if task.RequiresPlanApproval {
		r.logf("[session] plan requires approval; stopping before round 1\n")
		return Result{PlanPendingReview: true, Spec: spec, Usage: st.architect}, nil
	}
```

(Locate the exact line by searching for `r.save(ctx, st, 1, 0)` — there is exactly one call with those literal arguments, immediately following the Round-0 planning block. Insert directly after it, before whatever currently runs next — check the surrounding code first with `sed -n '530,560p' pkg/session/session.go`, since the plan text above was read at an earlier point in this same investigation and line numbers may have shifted slightly by the time Task 1-3 land.)

- [ ] **Step 4: Run the test, confirm it passes**

Run: `go test ./pkg/session/... -run TestRunStopsAfterPlanningWhenApprovalRequired -v`
Expected: PASS

- [ ] **Step 5: Run the full session suite to confirm the resume path still works untouched**

Run: `go test ./pkg/session/... -v`
Expected: PASS — in particular any existing resume/checkpoint test must still pass unmodified, since Task.RequiresPlanApproval defaults to `false` and the gate is a no-op for every existing caller.

- [ ] **Step 6: Confirm the licensing boundary still holds**

Run: `go test ./pkg/... -run TestLicensingBoundary -v` (exact test name from `pkg/licensing_boundary_test.go` — check it if this doesn't match)
Expected: PASS — this task adds no import to `pkg/session`.

- [ ] **Step 7: `gofmt -w pkg/session/` and commit**

```bash
git add pkg/session/session.go pkg/session/plan_review_test.go
git commit -m "feat(session): gate round 1 behind plan approval when requested"
```

---

### Task 5: Wire the gate through `pkg/daemon` and report `PLAN_REVIEW`

**Files:**
- Modify: `pkg/agent/agent.go` (`WorkerSpec`, add `RequiresPlanApproval`)
- Modify: `pkg/daemon/session_run.go` (the `session.Task{...}` literal at line 228, and the result handling below it, lines 237-260)
- Modify: `pkg/daemon/types.go` (`ResultReq`)
- Modify: `pkg/daemon/daemon.go` (`taskResult`, `reportResult`)
- Test: `pkg/daemon/session_run_plan_review_test.go`

**Interfaces:**
- Consumes: `session.Task.RequiresPlanApproval`, `session.Result.PlanPendingReview`/`.Spec` from Task 4.
- Produces: `agent.WorkerSpec.RequiresPlanApproval bool`; `ResultReq.Status == "PLAN_REVIEW"` with `ResultReq.PlanSpecJSON string` (the marshaled `session.Spec`) — consumed by Task 7 (`handleDaemonResult`).

- [ ] **Step 1: Write a failing test on `taskResult`/`reportResult` plumbing**

Follow the existing daemon test pattern for `executeSession` (check `pkg/daemon/session_run_test.go` for the fake sandbox/git/provider setup already used there — reuse it):

```go
func TestExecuteSessionReportsPlanReviewWithoutPublishing(t *testing.T) {
	// Build the same fixture executeSession's existing tests use, but with
	// spec.RequiresPlanApproval = true and a fake session.Architect that
	// returns a VerdictProposed spec on Plan().
	// ... (mirror the setup in session_run_test.go's existing happy-path test)

	result := d.executeSession(ctx, spec, creds, prog, deps)

	require.False(t, result.ok) // not a success — it's paused
	require.Equal(t, "PLAN_REVIEW", result.planReviewStatus)
	require.NotEmpty(t, result.planSpecJSON)
	// resolveGitToken / publishResultFrom must not have been called — no PR
	// is opened for a plan awaiting review.
}
```

- [ ] **Step 2: Run, confirm compile failure**

Run: `go test ./pkg/daemon/... -run TestExecuteSessionReportsPlanReviewWithoutPublishing -v`
Expected: FAIL — `spec.RequiresPlanApproval undefined`

- [ ] **Step 3: Add the field and thread it through**

In `pkg/agent/agent.go`, on `WorkerSpec` next to `ArchitectModel` (line ~106):

```go
	// RequiresPlanApproval, when set, makes the session stop after Round 0
	// planning and report PLAN_REVIEW instead of running the Implementer.
	RequiresPlanApproval bool `json:"requires_plan_approval,omitempty"`
```

In `pkg/daemon/session_run.go`, add `RequiresPlanApproval: spec.RequiresPlanApproval` to the `session.Task{...}` literal (line 228-235):

```go
	res, err := runner.Run(ctx, session.Task{
		ID:                   spec.ID,
		Description:          description,
		TestCmd:              deps.testCmd,
		InvestigationOnly:    spec.InvestigationOnly,
		RepoContext:          repoCtx,
		Learnings:            spec.Learnings,
		RequiresPlanApproval: spec.RequiresPlanApproval,
	})
```

Right after that call (before the existing `if err != nil { ... } else { ... }` logging block at line 237), add the early return for the paused case:

```go
	if res.PlanPendingReview {
		specJSON, merr := json.Marshal(res.Spec)
		if merr != nil {
			log.Printf("Task %s: failed to marshal plan spec: %v", spec.ID, merr)
			specJSON = []byte("{}")
		}
		return taskResult{detail: "plan pending review", events: prog.all(), planReviewStatus: store.TaskPlanReview, planSpecJSON: string(specJSON)}
	}
```

(`store` here means `github.com/ibreakthecloud/kiwi/pkg/store` — confirm it's already imported in `session_run.go`; it is, for other constants used in this file. `encoding/json` likewise.)

In `pkg/daemon/daemon.go`, extend `taskResult` (line 570-576):

```go
type taskResult struct {
	ok               bool
	prURL            string
	detail           string
	abuse            bool
	events           []ver.TaskEvent
	planReviewStatus string // set to store.TaskPlanReview instead of ok=true/false
	planSpecJSON     string // the marshaled session.Spec, forwarded to the Control Plane
}
```

And in `reportResult` (line 1088-1110), branch on it before the existing `ok`-based status derivation:

```go
	status := "SUCCEEDED"
	if out.planReviewStatus != "" {
		status = out.planReviewStatus
	} else if !out.ok {
		status = "FAILED"
	}
	sandboxRT := d.config.SandboxRuntime
	if sandboxRT == "" {
		sandboxRT = "docker"
	}
	req := ResultReq{
		TaskID:         taskID,
		LeaseID:        leaseID,
		Status:         status,
		SignPubKey:     base64.StdEncoding.EncodeToString(d.signPubKey),
		ResultURL:      out.prURL,
		Detail:         out.detail,
		Abuse:          out.abuse,
		Events:         out.events,
		SandboxRuntime: sandboxRT,
		PlanSpecJSON:   out.planSpecJSON,
	}
```

In `pkg/daemon/types.go`, add to `ResultReq` (after `SandboxRuntime`, line 83):

```go
	// PlanSpecJSON is the marshaled session.Spec, present only when Status is
	// "PLAN_REVIEW". An older Control Plane ignores an unknown field; an
	// older daemon simply never sends it.
	PlanSpecJSON string `json:"plan_spec_json,omitempty"`
```

- [ ] **Step 4: Run the test, confirm it passes**

Run: `go test ./pkg/daemon/... -run TestExecuteSessionReportsPlanReviewWithoutPublishing -v`
Expected: PASS

- [ ] **Step 5: Run the full daemon suite**

Run: `go test ./pkg/daemon/... -v`
Expected: PASS — every existing caller leaves `RequiresPlanApproval` at its zero value, so `executeSession`'s existing happy-path and failure tests are unaffected.

- [ ] **Step 6: `gofmt -w pkg/agent/ pkg/daemon/` and commit**

```bash
git add pkg/agent/agent.go pkg/daemon/session_run.go pkg/daemon/types.go pkg/daemon/daemon.go pkg/daemon/session_run_plan_review_test.go
git commit -m "feat(daemon): report PLAN_REVIEW without publishing a PR"
```

---

## Phase 3: Control Plane Handlers (ee/orchestrator)

### Task 6: Handle `PLAN_REVIEW` in `handleDaemonResult`

**Files:**
- Modify: `ee/orchestrator/daemon_api.go` (`handleDaemonResult`, starting line 365)
- Test: `ee/orchestrator/daemon_api_plan_review_test.go`

**Interfaces:**
- Consumes: `ResultReq.Status == "PLAN_REVIEW"`, `ResultReq.PlanSpecJSON` from Task 5; `Store.SetJobPlanPendingReview` from Task 3.
- Produces: a `renderPlanMarkdown(spec pkgsession.Spec) string` helper — consumed by nothing else in this plan, but is the one place plan-rendering logic lives.

- [ ] **Step 1: Write a failing HTTP-level test**

Follow the existing pattern in `ee/orchestrator/daemon_api_test.go` for posting a signed `ResultReq` to `/api/v1/daemon/result` (reuse its test daemon/signing helpers):

```go
func TestHandleDaemonResultPlanReview(t *testing.T) {
	// ... existing test scaffolding: register a daemon, enqueue+lease a task
	// belonging to a job with RequiresPlanApproval=true, as other tests in
	// this file already do.

	specJSON, _ := json.Marshal(session.Spec{
		Objective:          "add retry logic",
		AcceptanceCriteria: []string{"retries three times", "backs off exponentially"},
		MustChange:         []string{"pkg/client/retry.go"},
	})
	req := daemon.ResultReq{
		TaskID: taskID, LeaseID: leaseID, Status: "PLAN_REVIEW",
		SignPubKey: signPubKeyB64, Detail: "plan pending review",
		PlanSpecJSON: string(specJSON),
	}
	postSignedResult(t, mux, req, signPrivKey) // existing helper in this test file

	job, err := testStore.GetJob(context.Background(), jobID)
	require.NoError(t, err)
	require.Equal(t, "pending_review", job.PlanStatus)
	require.Equal(t, "PLAN_REVIEW", job.Status)
	require.Contains(t, job.PlanMarkdown, "add retry logic")
	require.Contains(t, job.PlanMarkdown, "retries three times")
}
```

- [ ] **Step 2: Run, confirm it fails**

Run: `go test ./ee/orchestrator/... -run TestHandleDaemonResultPlanReview -v`
Expected: FAIL — job's `PlanStatus` stays empty because `handleDaemonResult` doesn't branch on `PLAN_REVIEW` yet.

- [ ] **Step 3: Add the branch and the renderer**

Find where `handleDaemonResult` calls `s.storage.CompleteTask(...)` (search `daemon_api.go` from line 365 for `CompleteTask`) and add a branch before it — `PLAN_REVIEW` still calls `CompleteTask` (it's a valid `FinalStatus` per Task 2) to release the lease and meter cost/time, but additionally calls the new store method:

```go
	if req.Status == "PLAN_REVIEW" {
		var spec session.Spec
		if err := json.Unmarshal([]byte(req.PlanSpecJSON), &spec); err != nil {
			log.Printf("[daemon] task %s: invalid plan spec JSON: %v", req.TaskID, err)
		} else if err := s.storage.SetJobPlanPendingReview(r.Context(), task.JobID, renderPlanMarkdown(spec)); err != nil {
			log.Printf("[daemon] task %s: failed to record plan for review: %v", req.TaskID, err)
		}
	}
```

Place this after the existing `CompleteTask` call succeeds and after `task` (the `*store.QueuedTask` looked up earlier in the handler for org/lease resolution) is in scope — match the exact variable names already used in the surrounding function rather than introducing new ones.

Add the renderer near the bottom of the same file:

```go
// renderPlanMarkdown turns an Architect's Spec into the markdown a human
// reviews before approving. Kept in ee/orchestrator (not pkg/session) because
// it's a presentation concern, not an execution one.
func renderPlanMarkdown(spec session.Spec) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", spec.Objective)
	if len(spec.AcceptanceCriteria) > 0 {
		b.WriteString("## Acceptance criteria\n")
		for _, c := range spec.AcceptanceCriteria {
			fmt.Fprintf(&b, "- %s\n", c)
		}
		b.WriteString("\n")
	}
	if len(spec.MustChange) > 0 {
		b.WriteString("## Files expected to change\n")
		for _, f := range spec.MustChange {
			fmt.Fprintf(&b, "- `%s`\n", f)
		}
		b.WriteString("\n")
	}
	if len(spec.MustNotChange) > 0 {
		b.WriteString("## Must not change\n")
		for _, f := range spec.MustNotChange {
			fmt.Fprintf(&b, "- `%s`\n", f)
		}
		b.WriteString("\n")
	}
	if spec.Rationale != "" {
		fmt.Fprintf(&b, "## Rationale\n%s\n", spec.Rationale)
	}
	return b.String()
}
```

Add `"github.com/ibreakthecloud/kiwi/pkg/session"` and `"strings"` to `daemon_api.go`'s imports if not already present.

- [ ] **Step 4: Run the test, confirm it passes**

Run: `go test ./ee/orchestrator/... -run TestHandleDaemonResultPlanReview -v`
Expected: PASS

- [ ] **Step 5: Run the full daemon_api suite**

Run: `go test ./ee/orchestrator/... -run TestHandleDaemonResult -v`
Expected: PASS — every existing `SUCCEEDED`/`FAILED` test path is untouched by the new branch.

- [ ] **Step 6: Commit**

```bash
git add ee/orchestrator/daemon_api.go ee/orchestrator/daemon_api_plan_review_test.go
git commit -m "feat(orchestrator): record a job's plan for review on PLAN_REVIEW"
```

---

### Task 7: `GET /api/v1/jobs/{id}/plan`, `POST .../approve`, `POST .../reject`

**Files:**
- Create: `ee/orchestrator/plan_api.go`
- Modify: `ee/orchestrator/server.go` (route registration, near line 466 `mux.HandleFunc("/api/v1/jobs/", ...)`)
- Test: `ee/orchestrator/plan_api_test.go`

**Interfaces:**
- Consumes: `Store.GetJob`, `Store.ApproveJobPlan`, `Store.RejectJobPlan` (Task 3); `store.OriginPlanApproved` (Task 3); `store.EnqueueTask` (existing).
- Produces: the three HTTP endpoints from spec §3 Group A, response shapes as specified there.

This package's routing convention has no path-parameter matching (see `handleCancelMonitor`'s comment in `ee/orchestrator/monitors_api.go:91` for the established `TrimPrefix`/suffix idiom) — follow it rather than introducing a router library.

- [ ] **Step 1: Write failing HTTP tests for all three endpoints**

```go
package orchestrator

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestHandleGetJobPlan(t *testing.T) {
	s, mux := newTestServer(t) // existing helper used across this package's tests
	job := seedJobPendingReview(t, s, "org-1", "job-1") // helper: create a job, call SetJobPlanPendingReview

	req := authedRequest(t, http.MethodGet, "/api/v1/jobs/job-1/plan", nil, "org-1")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var resp struct {
		JobID            string `json:"job_id"`
		PlanStatus       string `json:"plan_status"`
		PlanMarkdown     string `json:"plan_markdown"`
		RequiresApproval bool   `json:"requires_approval"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Equal(t, "job-1", resp.JobID)
	require.Equal(t, "pending_review", resp.PlanStatus)
	require.True(t, resp.RequiresApproval)
}

func TestHandleGetJobPlanForbidsOtherOrg(t *testing.T) {
	s, mux := newTestServer(t)
	seedJobPendingReview(t, s, "org-1", "job-2")

	req := authedRequest(t, http.MethodGet, "/api/v1/jobs/job-2/plan", nil, "org-2")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	require.Equal(t, http.StatusForbidden, w.Code)
}

func TestHandleApproveJobPlanEnqueuesContinuation(t *testing.T) {
	s, mux := newTestServer(t)
	job := seedJobPendingReviewWithLeasedTask(t, s, "org-1", "job-3", "task-3a") // helper: also creates the QueuedTask CompleteTask'd as PLAN_REVIEW

	body, _ := json.Marshal(map[string]string{"user_comment": "looks right"})
	req := authedRequest(t, http.MethodPost, "/api/v1/jobs/job-3/plan/approve", bytes.NewReader(body), "org-1")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	j, err := s.GetJob(req.Context(), "job-3")
	require.NoError(t, err)
	require.Equal(t, "approved", j.PlanStatus)

	tasks, err := s.GetJobTasks(req.Context(), "org-1", "job-3")
	require.NoError(t, err)
	require.Len(t, tasks, 2) // the original PLAN_REVIEW task plus the continuation
	var continuation *store.QueuedTask
	for i := range tasks {
		if tasks[i].Origin == store.OriginPlanApproved {
			continuation = &tasks[i]
		}
	}
	require.NotNil(t, continuation)
	require.NotNil(t, continuation.ParentTaskID)
	require.Equal(t, "task-3a", *continuation.ParentTaskID)
}

func TestHandleRejectJobPlan(t *testing.T) {
	s, mux := newTestServer(t)
	seedJobPendingReview(t, s, "org-1", "job-4")

	body, _ := json.Marshal(map[string]string{"feedback": "wrong approach"})
	req := authedRequest(t, http.MethodPost, "/api/v1/jobs/job-4/plan/reject", bytes.NewReader(body), "org-1")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	j, err := s.GetJob(req.Context(), "job-4")
	require.NoError(t, err)
	require.Equal(t, "rejected", j.PlanStatus)
	require.Equal(t, "wrong approach", j.PlanRejectedReason)
}
```

Write `seedJobPendingReview`, `seedJobPendingReviewWithLeasedTask`, and `authedRequest` as local test helpers in `plan_api_test.go` if this package's existing tests don't already provide equivalents — check `ee/orchestrator/jobs_api_test.go` and `ee/orchestrator/daemon_api_test.go` first, since both likely already build authenticated requests and seeded jobs for their own tests.

- [ ] **Step 2: Run, confirm they fail**

Run: `go test ./ee/orchestrator/... -run 'TestHandleGetJobPlan|TestHandleApproveJobPlan|TestHandleRejectJobPlan' -v`
Expected: FAIL — 404s, routes don't exist yet.

- [ ] **Step 3: Implement the handlers**

```go
// SPDX-License-Identifier: LicenseRef-Kiwi-BSL-1.1
// Copyright (c) 2026 RunKiwi. Licensed under the Business Source License 1.1.
// See ee/LICENSE. This is Control Plane code and is NOT Apache-2.0.

package orchestrator

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/ibreakthecloud/kiwi/ee/auth"
	"github.com/ibreakthecloud/kiwi/pkg/store"
)

// handleJobPlan serves GET /api/v1/jobs/{id}/plan,
// POST /api/v1/jobs/{id}/plan/approve, and POST /api/v1/jobs/{id}/plan/reject.
// Registered at the "/api/v1/jobs/" prefix already owned by handleJobStatus;
// dispatch here on the "/plan" suffix before falling through.
func (s *Server) handleJobPlan(w http.ResponseWriter, r *http.Request, jobID, action string) {
	claims := auth.ClaimsFromContext(r.Context())
	if claims == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	job, err := s.storage.GetJob(r.Context(), jobID)
	if err != nil {
		http.Error(w, "Job not found", http.StatusNotFound)
		return
	}
	if job.OrgID != claims.OrgID {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	switch action {
	case "":
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"job_id":            job.ID,
			"plan_status":       job.PlanStatus,
			"plan_markdown":     job.PlanMarkdown,
			"requires_approval": job.RequiresPlanApproval,
			"architect_model":   job.ArchitectModel,
			"created_at":        job.CreatedAt,
		})
	case "approve":
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		s.handleApproveJobPlan(w, r, job)
	case "reject":
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		s.handleRejectJobPlan(w, r, job)
	default:
		http.Error(w, "Not found", http.StatusNotFound)
	}
}

func (s *Server) handleApproveJobPlan(w http.ResponseWriter, r *http.Request, job *store.Job) {
	if job.PlanStatus != "pending_review" {
		http.Error(w, "plan is not pending review", http.StatusConflict)
		return
	}

	tasks, err := s.storage.GetJobTasks(r.Context(), job.OrgID, job.ID)
	if err != nil || len(tasks) == 0 {
		http.Error(w, "could not locate the paused task", http.StatusInternalServerError)
		return
	}
	// The task the daemon reported PLAN_REVIEW on is the most recent one on
	// the job's root thread — the same lookup pattern used to find the active
	// task in a PR-comment continuation (see ActiveTaskInThread).
	var parent *store.QueuedTask
	for i := range tasks {
		if tasks[i].Status == store.TaskPlanReview {
			parent = &tasks[i]
		}
	}
	if parent == nil {
		http.Error(w, "no plan-review task found for this job", http.StatusConflict)
		return
	}

	if err := s.storage.ApproveJobPlan(r.Context(), job.ID); err != nil {
		http.Error(w, "failed to approve plan", http.StatusInternalServerError)
		return
	}

	continuation := &store.QueuedTask{
		ID:           generateTaskID(), // reuse whatever ID generator jobs_api.go's continuation path already uses
		OrgID:        job.OrgID,
		JobID:        job.ID,
		ParentTaskID: &parent.ID,
		RootTaskID:   parent.RootTaskID,
		Origin:       store.OriginPlanApproved,
		Status:       store.TaskQueued,
		Spec:         parent.Spec, // same worker-spec: same repo, model, test command, SessionID
		FleetID:      parent.FleetID,
	}
	if err := s.storage.EnqueueTask(r.Context(), continuation); err != nil {
		http.Error(w, "failed to enqueue continuation", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"status": "approved", "resumed_phase": "actor"})
}

func (s *Server) handleRejectJobPlan(w http.ResponseWriter, r *http.Request, job *store.Job) {
	if job.PlanStatus != "pending_review" {
		http.Error(w, "plan is not pending review", http.StatusConflict)
		return
	}
	var body struct {
		Feedback string `json:"feedback"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)

	if err := s.storage.RejectJobPlan(r.Context(), job.ID, body.Feedback); err != nil {
		http.Error(w, "failed to reject plan", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"status": "rejected", "planner_notified": true})
}
```

Before writing `generateTaskID()`, check how `jobs_api.go` or `postmerge_finalize.go` already mint a `QueuedTask.ID` for a continuation (both were flagged earlier as already creating `OriginPRComment`-style continuations) and call that same function — do not add a second ID generator.

Wire the dispatch into the existing `/api/v1/jobs/` handler. Find `handleJobStatus` (`jobs_api.go:113`) and, if it doesn't already, have `server.go`'s registration route `/plan`-suffixed paths to the new handler instead — the cleanest fit is adding the suffix check inside `handleJobStatus` itself (it already parses the job ID out of the path) rather than a second `mux.HandleFunc`, matching how `handleCancelMonitor` shares its prefix with `handleListMonitors`. Read `handleJobStatus`'s path-parsing in full before deciding; if it cleanly exposes the remainder of the path after the job ID, branch there:

```go
	// inside handleJobStatus, after the job ID is parsed out of the path:
	if rest == "plan" || strings.HasPrefix(rest, "plan/") {
		action := strings.TrimPrefix(strings.TrimPrefix(rest, "plan"), "/")
		s.handleJobPlan(w, r, jobID, action)
		return
	}
```

- [ ] **Step 4: Run the tests, confirm they pass**

Run: `go test ./ee/orchestrator/... -run 'TestHandleGetJobPlan|TestHandleApproveJobPlan|TestHandleRejectJobPlan' -v`
Expected: PASS

- [ ] **Step 5: Run the full orchestrator suite**

Run: `go test ./ee/orchestrator/... -v`
Expected: PASS — `handleJobStatus`'s existing tests must still pass since `plan` is a new, previously-404 suffix.

- [ ] **Step 6: `gofmt -w ee/orchestrator/` and commit**

```bash
git add ee/orchestrator/plan_api.go ee/orchestrator/plan_api_test.go ee/orchestrator/jobs_api.go
git commit -m "feat(orchestrator): add plan review, approve, and reject endpoints"
```

---

### Task 8: `PUT /api/v1/jobs/{id}/spend-cap`

**Files:**
- Modify: `ee/orchestrator/plan_api.go` (add `handleJobSpendCap`, dispatched the same way as Task 7)
- Test: `ee/orchestrator/plan_api_test.go` (add to the same file)

**Interfaces:**
- Consumes: `Store.SetJobSpendCap` (Task 3).
- Produces: `PUT /api/v1/jobs/{id}/spend-cap` per spec §3 Group D.

- [ ] **Step 1: Write a failing test**

```go
func TestHandleJobSpendCap(t *testing.T) {
	s, mux := newTestServer(t)
	seedPlainJob(t, s, "org-1", "job-5") // any job; RequiresPlanApproval irrelevant here

	body, _ := json.Marshal(map[string]float64{"spend_cap_usd": 1.5})
	req := authedRequest(t, http.MethodPut, "/api/v1/jobs/job-5/spend-cap", bytes.NewReader(body), "org-1")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	j, err := s.GetJob(req.Context(), "job-5")
	require.NoError(t, err)
	require.Equal(t, 1.5, j.SpendCapUSD)
}
```

- [ ] **Step 2: Run, confirm it fails (404)**

Run: `go test ./ee/orchestrator/... -run TestHandleJobSpendCap -v`

- [ ] **Step 3: Implement**

```go
func (s *Server) handleJobSpendCap(w http.ResponseWriter, r *http.Request, orgID, jobID string) {
	if r.Method != http.MethodPut {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		SpendCapUSD float64 `json:"spend_cap_usd"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.SpendCapUSD < 0 {
		http.Error(w, "Bad request: 'spend_cap_usd' must be a non-negative number", http.StatusBadRequest)
		return
	}
	if err := s.storage.SetJobSpendCap(r.Context(), orgID, jobID, body.SpendCapUSD); err != nil {
		if err == store.ErrJobNotFound {
			http.Error(w, "Job not found", http.StatusNotFound)
			return
		}
		http.Error(w, "failed to set spend cap", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"job_id": jobID, "spend_cap_usd": body.SpendCapUSD})
}
```

Dispatch it the same way Task 7 dispatched `/plan`, adding a `rest == "spend-cap"` branch in `handleJobStatus` (or wherever Task 7 ended up placing the dispatch) using `claims.OrgID` for the `orgID` argument.

- [ ] **Step 4: Run the test, confirm pass**

Run: `go test ./ee/orchestrator/... -run TestHandleJobSpendCap -v`
Expected: PASS

- [ ] **Step 5: Full suite + gofmt, commit**

```bash
gofmt -w ee/orchestrator/
go test ./ee/orchestrator/... -v
git add ee/orchestrator/plan_api.go ee/orchestrator/plan_api_test.go ee/orchestrator/jobs_api.go
git commit -m "feat(orchestrator): add per-job spend cap endpoint"
```

---

## Verification & Handoff Checklist

1. [ ] `gofmt -l cmd/ pkg/ ee/` returns 0 modified files.
2. [ ] `go test ./pkg/...` passes 100%.
3. [ ] `go test ./ee/...` passes 100%.
4. [ ] `go build ./...` succeeds.
5. [ ] `go test ./pkg/... -run TestLicensingBoundary` (or the actual test name in `pkg/licensing_boundary_test.go`) still passes — `pkg/session`, `pkg/agent`, `pkg/daemon` import nothing under `ee/`.
6. [ ] Every existing endpoint response shape is unchanged for jobs that never set `RequiresPlanApproval` — spot-check `GET /api/v1/jobs/{id}` (`handleJobStatus`) still omits the new fields cleanly (they're `omitempty`/zero-valued).
7. [ ] A job with `RequiresPlanApproval=false` end-to-end: submit → session runs all rounds → PR opened, with zero behavior change from before this plan (this is the regression check for the entire `pkg/session` gate — Task 4/5's tests cover it directly, but re-run `go test ./pkg/daemon/... ./pkg/session/... -v` one more time here as a checkpoint).
