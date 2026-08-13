# Live progress visibility: closing the silent stretches

Status: approved for implementation
Date: 2026-08-13

## Problem

Investigating `job_0e1729204e4497e5` (a run that burned its entire 20-minute
Free-tier budget on a baseline test before ever calling a model) and
`job_e3a491f48809d606` (a representative qwen-plus run) established that the
live progress feed already covers most of a session, but three gaps remain,
all confirmed by tracing `setActivity` call sites against what actually
reaches the dashboard:

1. **Setup is invisible, live and in history.** `executeTask` in
   `pkg/daemon/daemon.go` clones the repo (~line 483) and runs the
   repository's own install command (~line 596) before the session loop
   starts. Neither call site calls `prog.setActivity`, so `LiveRun.tsx` shows
   a generic "working" orb with no phase or command during this stretch — on
   `job_e3a491f48809d606` this was part of a ~2-minute block with zero
   signal. Because nothing is ever written to `task_events` for this stretch
   either, a finished run's timeline has no row for it — the gap is
   permanent, not just a live-view artifact.
2. **Only completion is durable.** `pkg/session/session.go`'s `r.emit` (and
   the mirroring `OnEvent` in `pkg/daemon/session_run.go:202`) write a
   `task_events` row once a phase finishes, never when it starts. The
   baseline test and Architect plan/review already showed this: on
   `job_e3a491f48809d606`, `test` and `critic` phases each produced exactly
   one row, timestamped at completion, with the full duration folded in —
   confirmed by the `max_gap_s == 126.0` finding, which exactly matched the
   plan phase's own duration.
3. **The live "now" line doesn't say how long it's been running.** Where
   `setActivity` *is* called (baseline test, plan, review, tool calls), the
   label refreshes every 3s but only ever shows what phase is active, never
   for how long. A 4-minute plan and a just-started one render identically
   apart from the "no update for Ns" staleness warning, which fires only
   past 30s of the *feed* going quiet — not the same thing as the phase
   itself being long-running.

## Goals

- Give the setup phase (clone, initial dependency install) the same live
  signal every other phase already has.
- Persist that same setup phase into the durable `task_events` history, so
  it shows up in `RunTimeline` for a finished run, not just while it's
  running.
- Show elapsed time on whatever phase is currently active, in the live view.

## Non-goals

- **Mid-round re-installs are not getting a history row in this change.**
  `session.VerifyFunc` (`pkg/session/session.go:65`) is
  `func(ctx) (output string, passed bool, err error)` — no round number is
  available at the re-install call site (`pkg/daemon/daemon.go:661`), and
  threading one through would mean changing that interface. The re-install
  keeps its existing live-only visibility (`daemon.go:661-665`, unchanged).
  Revisit only if it turns out to matter in practice.
- Not changing anything about the tool-call loop — it already streams
  correctly and needs no work here.
- Not adding true sub-phase progress inside a single LLM call (no streaming
  token counts). Elapsed time is the signal; that's deliberately the
  ceiling for what's observable about a synchronous model call.

## Design

### 1. Setup-phase live visibility

In `pkg/daemon/daemon.go:executeTask`, `prog *progressReporter` is already a
parameter and in scope at both dark call sites. Add:

- Before `d.gitCache.GetJobWorktree` (~line 483):
  `prog.setActivity("clone: "+spec.RepoURL, "")`
- Before the initial `d.installDependencies` call (~line 596):
  `prog.setActivity("install: "+step.Command, "")`

This mirrors the existing pattern at `daemon.go:661` (`prog.setActivity("install: "+step.Command, "")`
for the mid-round re-install) — same phase-string shape, so
`LiveRun.tsx`'s `splitPhase` needs no changes.

### 2. Persisted history for the setup phase

Time each of the two setup calls and, on completion, call
`prog.add(ver.TaskEvent{...})` directly — the same mechanism
`session_run.go:203`'s `OnEvent` closure already uses, but invoked straight
from `daemon.go` since this happens before the `session.Runner` exists.

- `Phase: "clone"`, `Step: 0`, `Detail: spec.RepoURL`, `DurationMs` timed
  around `GetJobWorktree`, `Outcome: "ok"` or `"error"`.
- `Phase: "install"`, `Step: 0`, `Detail: step.Command`, `DurationMs` timed
  around `installDependencies`, `Outcome` from its `ok` return.

`Step: 0` groups both under the timeline's existing "Start" bucket
(`RunTimeline.tsx:301`, `step === 0 ? "Start" : ...`), ahead of the baseline
`test` event, which is also `Step: 0` today (`session_run.go:204`,
`Step: e.Round`, and the baseline verify runs at round 0). Insertion order
in `progressReporter.events` is append-only, so the two setup rows land
before the baseline test row without any explicit ordering field.

`RunTimeline.tsx`'s `PHASE_LABEL` map (line 74) gets two new entries:

```
clone:   "Cloning repository",
install: "Installing dependencies",
```

### 3. Elapsed time on the live "now" line

Add a `phaseSince time.Time` field to `progressReporter`
(`pkg/daemon/progress.go`). In `setActivity`, set it only when the incoming
phase string differs from the current one:

```go
func (p *progressReporter) setActivity(phase, output string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if phase != p.phase {
		p.phaseSince = time.Now().UTC()
	}
	p.phase = phase
	p.tail = outputTail(output, maxTailBytes)
}
```

This is why the two `setActivity("test: "+testCmd, ...)` calls in
`daemon.go:673` and `:678` (before and after the same test run) must not
reset the clock — they carry identical phase text, only the tail differs.

Thread it through:

- `progressReporter.pending()` returns `phaseSince` alongside `phase`/`tail`.
- `daemon.ProgressReq` (`pkg/daemon/types.go:100`) gains
  `PhaseSince time.Time \`json:"phase_since,omitempty"\``.
- `PostgresStore.RecordTaskProgress` (`pkg/store/queue_lifecycle.go:190`)
  gains a `phaseSince time.Time` parameter and writes it to a new
  `progress_phase_since` column, following the same
  "only write when non-zero" guard the existing `phase`/`output` params use.
- `store.QueuedTask` (`pkg/store/queue_models.go:92`) gains
  `ProgressPhaseSince *time.Time \`json:"progress_phase_since"\``.
- Migration `0034_task_progress_phase_since` adds the column, copying the
  guarded pattern from `migrations/0020_task_progress_columns.up.sql`
  exactly: `IF to_regclass('public.queued_tasks') IS NOT NULL` wrapping
  `ADD COLUMN IF NOT EXISTS`. **This guard is load-bearing, not
  boilerplate** — 0020's own commit message records that skipping it once
  already took down task submission in production, because
  `queued_tasks` exists only via `AutoMigrate` (off in prod) and a
  GORM-struct-only field with no migration silently diverges from the
  live schema until the first `INSERT`/`UPDATE` naming the missing column
  fails. Down migration is a no-op for the same reason 0020's is: no data
  worth protecting, and a daemon mid-flight must not break against a
  rolled-back column.
- `handleDaemonProgress` (`ee/orchestrator/daemon_api.go:811`) passes
  `req.PhaseSince` through to `RecordTaskProgress`.
- `jobProgressTask` (`ee/orchestrator/jobs_api.go`, alongside `ProgressAt`
  at line 273) gains `PhaseSince *time.Time \`json:"phase_since,omitempty"\``,
  populated from `t.ProgressPhaseSince` in `handleJobProgress`.
- `frontend/src/lib/api.ts`'s `JobProgressTask` type gains
  `phase_since?: string`.
- `LiveRun.tsx` gets an `elapsedSince()` helper, the same shape as the
  existing `staleness()` (line 30), and renders it unconditionally next to
  the phase label when present — not gated behind a threshold, since "40s
  elapsed" on a healthy run is exactly the information that distinguishes
  "just started" from "four minutes in." This is a different signal from
  the existing "no update for Ns" warning: staleness asks whether the feed
  is still arriving; elapsed asks how long the current step has taken.

## Testing

- `pkg/daemon/progress_test.go` (new or extended): `setActivity` resets
  `phaseSince` only when the phase string changes, not on a same-phase
  tail-only update.
- `pkg/store/queue_lifecycle_test.go`: `RecordTaskProgress` persists
  `progress_phase_since` and leaves it untouched when the call omits it.
- `pkg/daemon/daemon_test.go` (or wherever `executeTask` setup is already
  covered): clone and initial install each produce one `ver.TaskEvent` with
  `Step: 0` and the expected `Phase`.
- `frontend/src/lib/orbState.test.ts`-adjacent test for `elapsedSince()`:
  same-shape unit test as whatever covers `staleness()` today.
- Migration: apply `0034` against a fresh schema and against one already
  carrying the column (the `IF NOT EXISTS` guard), matching how 0020 is
  presumably exercised.

## Rollout

No flag. This is additive telemetry — new columns default `NULL`, new
`task_events` phases are new rows a client that doesn't recognize `clone`/
`install` will still render (via `PHASE_LABEL`'s `?? phase` fallback,
`RunTimeline.tsx:88`, `:104`) as the raw phase string until the frontend
change ships alongside it.
