# Live Progress Visibility Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Close the three silent stretches found while tracing `job_0e1729204e4497e5` — setup (clone/install) has no live or historical signal, only phase-completion is ever persisted, and the live "now" line never says how long the current phase has been running.

**Architecture:** Extend the existing `progressReporter` (daemon) → `ProgressReq` (HTTP) → `queued_tasks` columns (Postgres) → `jobProgressTask` (API) → `LiveRun.tsx` (frontend) pipeline that already carries the live phase indicator, adding one new field (`phase_since`, for elapsed time) and two new call sites in `executeTask` (for the setup phase, live + persisted history).

**Tech Stack:** Go 1.25 (daemon, orchestrator, store), PostgreSQL migration, TypeScript/React (Next.js frontend), `node --test` for frontend unit tests.

## Global Constraints

- `gofmt -l cmd/ pkg/ ee/` must print nothing; `CGO_ENABLED=0 go vet ./...`, `CGO_ENABLED=0 go test ./pkg/...`, `CGO_ENABLED=0 go build ./...` must all pass before any commit.
- `ee/` is BSL 1.1; `pkg/` and `cmd/` are Apache-2.0. Nothing in this plan adds an `ee/`-to-`pkg/` import in the wrong direction — `pkg/daemon` and `pkg/store` stay import-free of `ee/`.
- Migration files must guard `queued_tasks` DDL with `IF to_regclass('public.queued_tasks') IS NOT NULL` and `ADD COLUMN IF NOT EXISTS`, exactly as `migrations/0020_task_progress_columns.up.sql` does — `queued_tasks` exists only via `AutoMigrate`, which is off in production, so an unguarded migration or a GORM-struct-only field silently diverges from the live schema until an INSERT/UPDATE naming the missing column fails in prod.
- Every new feature ships with tests first (TDD): write the failing test, watch it fail, then implement.
- Spec: `docs/superpowers/specs/2026-08-13-live-progress-visibility-design.md`.

---

### Task 1: `progressReporter` tracks elapsed time on the current phase

**Files:**
- Modify: `pkg/daemon/progress.go`
- Modify: `pkg/daemon/progress_test.go`
- Modify: `pkg/daemon/types.go:100-113` (`ProgressReq`)

**Interfaces:**
- Produces: `progressReporter.pending()` now returns `(events []ver.TaskEvent, phase, tail string, phaseSince time.Time, upto int)` — a 5th return value inserted before `upto`. Every existing caller must be updated in this task.
- Produces: `ProgressReq.PhaseSince time.Time` (JSON `phase_since`, `omitempty`).

- [ ] **Step 1: Write the failing test for phase-change detection**

Add to `pkg/daemon/progress_test.go`:

```go
// The elapsed clock resets only when the phase itself changes — not on every
// setActivity call. daemon.go calls setActivity twice for the same command
// (once before it runs, once after with the output tail attached), and that
// second call must not make a phase look like it just started.
func TestProgress_PhaseSinceResetsOnlyOnPhaseChange(t *testing.T) {
	p := &progressReporter{}

	p.setActivity("test: go test ./...", "")
	_, _, _, firstSince, _ := p.pending()
	if firstSince.IsZero() {
		t.Fatal("phaseSince should be set on the first activity")
	}

	p.setActivity("test: go test ./...", "some output")
	_, _, _, sameSince, _ := p.pending()
	if !sameSince.Equal(firstSince) {
		t.Errorf("same phase text should not reset phaseSince: got %v, want %v", sameSince, firstSince)
	}

	p.setActivity("install: npm ci", "")
	_, _, _, newSince, _ := p.pending()
	if !newSince.After(firstSince) {
		t.Errorf("a genuinely new phase should advance phaseSince: got %v, want after %v", newSince, firstSince)
	}
}
```

- [ ] **Step 2: Update every existing `pending()` call site to the new 5-value signature**

In `pkg/daemon/progress_test.go`, change each of the following (8 call sites — the 9th, in `progress.go`'s `streamProgress`, is handled in Step 5) to add the 4th blank/binding before `upto`/final blank:

```go
// TestProgress_SendsOnlyTheDelta
events, _, _, _, upto := p.pending()
...
if events, _, _, _, _ := p.pending(); len(events) != 0 {
...
events, _, _, _, _ = p.pending()

// TestProgress_UnacknowledgedEventsSurviveAFailedFlush
events, _, _, _, _ := p.pending() // flush "fails": no ack
...
again, _, _, _, _ := p.pending()

// TestProgress_AllReturnsTheFullHistory
_, _, _, _, upto := p.pending()

// TestProgress_OutputTailIsBoundedAndKeepsTheEnd
_, phase, tail, _, _ := p.pending()

// TestProgress_ConcurrentAddAndFlush
_, _, _, _, upto := p.pending()
```

- [ ] **Step 3: Run the tests to verify they fail (compile error until Step 4)**

Run: `CGO_ENABLED=0 go test ./pkg/daemon/... -run TestProgress -v`
Expected: FAIL to compile — `pending` still returns 4 values.

- [ ] **Step 4: Implement `phaseSince` in `progressReporter`**

In `pkg/daemon/progress.go`, add the field to the struct (after `phase string`):

```go
type progressReporter struct {
	mu     sync.Mutex
	events []ver.TaskEvent
	sent  int
	tail  string
	phase string
	// phaseSince is when the current phase started, set only when the phase
	// text actually changes — a long command reports the same phase text
	// across several setActivity calls (before it runs, then with its output
	// tail attached), and none of those should make it look freshly started.
	phaseSince time.Time
}
```

Update `setActivity`:

```go
// setActivity records what is running now and the tail of its output, so a long
// command reports something other than silence. phaseSince advances only when
// the phase itself changes, so the live view can say how long the CURRENT
// phase has taken — not just that something is happening.
func (p *progressReporter) setActivity(phase, output string) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if phase != p.phase {
		p.phaseSince = time.Now().UTC()
	}
	p.phase = phase
	p.tail = outputTail(output, maxTailBytes)
}
```

Update `pending`:

```go
// pending returns the events not yet accepted, plus the current activity and
// when that activity started.
func (p *progressReporter) pending() (events []ver.TaskEvent, phase, tail string, phaseSince time.Time, upto int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	upto = len(p.events)
	if p.sent < upto {
		events = append(events, p.events[p.sent:upto]...)
	}
	return events, p.phase, p.tail, p.phaseSince, upto
}
```

- [ ] **Step 5: Update `streamProgress`'s call site and the `ProgressReq` it sends**

In `pkg/daemon/progress.go`, `streamProgress`:

```go
case <-ticker.C:
	events, phase, tail, phaseSince, upto := p.pending()
	if len(events) == 0 && phase == "" {
		continue
	}
	if err := d.client.ReportProgress(ctx, ProgressReq{
		TaskID:     taskID,
		LeaseID:    leaseID,
		SignPubKey: d.signPubKeyB64(),
		Events:     events,
		Phase:      phase,
		OutputTail: tail,
		PhaseSince: phaseSince,
	}); err != nil {
```

In `pkg/daemon/types.go`, add to `ProgressReq` (after `OutputTail`):

```go
	// PhaseSince is when the current Phase started, so the dashboard can show
	// how long this step has actually been running — not just that the feed
	// is still alive (which ProgressAt on the receiving end already answers).
	PhaseSince time.Time `json:"phase_since,omitempty"`
```

Add `"time"` to `pkg/daemon/types.go`'s imports if not already present.

- [ ] **Step 6: Run the tests to verify they pass**

Run: `CGO_ENABLED=0 go test ./pkg/daemon/... -run TestProgress -v`
Expected: PASS, all `TestProgress_*` tests including the new one.

- [ ] **Step 7: Run the full daemon package build and vet**

Run: `CGO_ENABLED=0 go vet ./pkg/daemon/... && CGO_ENABLED=0 go build ./pkg/daemon/...`
Expected: clean.

- [ ] **Step 8: Commit**

```bash
git add pkg/daemon/progress.go pkg/daemon/progress_test.go pkg/daemon/types.go
git commit -m "feat(daemon): track elapsed time on the current progress phase"
```

---

### Task 2: Persist `phase_since` on `queued_tasks`

**Files:**
- Create: `migrations/0034_task_progress_phase_since.up.sql`
- Create: `migrations/0034_task_progress_phase_since.down.sql`
- Modify: `pkg/store/queue_models.go:88-94` (`QueuedTask`)
- Modify: `pkg/store/queue_lifecycle.go:190-210` (`RecordTaskProgress`)
- Modify: `pkg/store/store.go:106-108` (`Store` interface)
- Modify: `pkg/store/queue_lifecycle_test.go`

**Interfaces:**
- Consumes: nothing from Task 1 directly (this task can be built independently; Task 3 wires the two together).
- Produces: `PostgresStore.RecordTaskProgress(ctx, taskID, leaseID, phase, output string, phaseSince time.Time) (bool, error)` — signature gains a 6th parameter. `store.QueuedTask.ProgressPhaseSince *time.Time`.

- [ ] **Step 1: Write the failing test**

Add to `pkg/store/queue_lifecycle_test.go`:

```go
// RecordTaskProgress must persist when the current phase started, not just
// what it is — the dashboard uses this to show how long a step has been
// running, distinct from ProgressAt (when the daemon last reported at all).
func TestRecordTaskProgress_PersistsPhaseSince(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if err := s.EnqueueTask(ctx, &QueuedTask{
		ID: "j1-a", OrgID: "org1", JobID: "j1",
		Spec: map[string]interface{}{"task": "fix it"},
	}); err != nil {
		t.Fatal(err)
	}
	registerDaemon(t, s, "d1", "org1", "", nil)
	leased, err := s.LeaseNextTask(ctx, "org1", "d1", "", time.Minute)
	if err != nil || leased == nil {
		t.Fatalf("lease: %v %v", leased, err)
	}
	leaseID := *leased.LeaseID

	since := time.Now().Add(-90 * time.Second).UTC().Truncate(time.Second)
	ok, err := s.RecordTaskProgress(ctx, "j1-a", leaseID, "install: npm ci", "", since)
	if err != nil {
		t.Fatalf("RecordTaskProgress: %v", err)
	}
	if !ok {
		t.Fatal("expected the write to apply")
	}

	got := statusOfTask(t, s, "j1-a")
	if got.ProgressPhaseSince == nil {
		t.Fatal("ProgressPhaseSince was not persisted")
	}
	if !got.ProgressPhaseSince.Equal(since) {
		t.Errorf("ProgressPhaseSince = %v, want %v", got.ProgressPhaseSince, since)
	}
}

// A phase update with a zero PhaseSince (the caller has nothing new to say
// about timing) must not overwrite a previously recorded value with NULL.
func TestRecordTaskProgress_ZeroPhaseSinceLeavesExistingValueAlone(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if err := s.EnqueueTask(ctx, &QueuedTask{
		ID: "j1-b", OrgID: "org1", JobID: "j1",
		Spec: map[string]interface{}{"task": "fix it"},
	}); err != nil {
		t.Fatal(err)
	}
	registerDaemon(t, s, "d2", "org1", "", nil)
	leased, err := s.LeaseNextTask(ctx, "org1", "d2", "", time.Minute)
	if err != nil || leased == nil {
		t.Fatalf("lease: %v %v", leased, err)
	}
	leaseID := *leased.LeaseID

	since := time.Now().UTC().Truncate(time.Second)
	if _, err := s.RecordTaskProgress(ctx, "j1-b", leaseID, "install: npm ci", "", since); err != nil {
		t.Fatalf("first RecordTaskProgress: %v", err)
	}
	if _, err := s.RecordTaskProgress(ctx, "j1-b", leaseID, "install: npm ci", "more output", time.Time{}); err != nil {
		t.Fatalf("second RecordTaskProgress: %v", err)
	}

	got := statusOfTask(t, s, "j1-b")
	if got.ProgressPhaseSince == nil || !got.ProgressPhaseSince.Equal(since) {
		t.Errorf("ProgressPhaseSince = %v, want unchanged %v", got.ProgressPhaseSince, since)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `CGO_ENABLED=0 go test ./pkg/store/... -run TestRecordTaskProgress_PersistsPhaseSince -v`
Expected: FAIL to compile — `RecordTaskProgress` does not take a 6th argument yet, and `ProgressPhaseSince` does not exist on `QueuedTask`.

- [ ] **Step 3: Add the column to the migrations directory**

`migrations/0034_task_progress_phase_since.up.sql`:

```sql
-- phase_since: when the daemon's currently-reported phase started, so the
-- dashboard can show how long the current step has been running rather than
-- only that the feed is still alive (progress_at already answers the
-- latter). Same guard as 0020_task_progress_columns.up.sql, and for the same
-- reason: queued_tasks exists only via AutoMigrate, which is off in
-- production, so a GORM-struct-only field with no migration silently
-- diverges from the live schema until an INSERT/UPDATE naming the missing
-- column fails in prod. IF NOT EXISTS keeps this safe on a database where
-- AutoMigrate did run and already created it.
DO $$
BEGIN
  IF to_regclass('public.queued_tasks') IS NOT NULL THEN
    ALTER TABLE queued_tasks ADD COLUMN IF NOT EXISTS progress_phase_since TIMESTAMPTZ;
  END IF;
END $$;
```

`migrations/0034_task_progress_phase_since.down.sql`:

```sql
-- Dropping this would break any daemon still reporting progress against it,
-- and it holds no data worth protecting — a run's authoritative history
-- lives in task_events. Deliberately a no-op, matching 0020's down migration.
SELECT 1;
```

- [ ] **Step 4: Add the field to `QueuedTask`**

In `pkg/store/queue_models.go`, after `ProgressAt`:

```go
	ProgressPhase  *string    `json:"progress_phase"`
	ProgressOutput *string    `json:"progress_output"`
	ProgressAt     *time.Time `json:"progress_at"`
	// ProgressPhaseSince is when ProgressPhase started, distinct from
	// ProgressAt (when the daemon last reported anything at all). Set once per
	// phase change and left alone on every subsequent report of the same
	// phase — see progressReporter.setActivity in pkg/daemon.
	ProgressPhaseSince *time.Time `json:"progress_phase_since"`
```

- [ ] **Step 5: Update `RecordTaskProgress`**

In `pkg/store/queue_lifecycle.go`:

```go
// RecordTaskProgress stores what a daemon says it is doing right now.
//
// Guarded by the fencing token, exactly as CompleteTask and RenewLease are: a
// daemon whose lease was reassigned must not be able to write progress onto a
// task another daemon now owns, or the dashboard would show one run's output
// under another's. It also refuses to touch a task that is no longer LEASED, so
// a late update cannot overwrite a finished task's final state.
//
// Returns false when the write did not apply, which the caller treats as
// informational — progress is best-effort and must never fail a run.
func (s *PostgresStore) RecordTaskProgress(ctx context.Context, taskID, leaseID, phase, output string, phaseSince time.Time) (bool, error) {
	now := time.Now()
	updates := map[string]interface{}{
		"progress_at": &now,
	}
	if phase != "" {
		updates["progress_phase"] = &phase
	}
	if output != "" {
		updates["progress_output"] = &output
	}
	// A zero PhaseSince means the caller has nothing new to say about timing
	// (e.g. an output-only update on an unchanged phase) — leaving the column
	// untouched keeps the previously recorded start time, rather than the
	// write racing it back to NULL.
	if !phaseSince.IsZero() {
		updates["progress_phase_since"] = &phaseSince
	}

	res := s.db.WithContext(ctx).
		Model(&QueuedTask{}).
		Where("id = ? AND lease_id = ? AND status = ?", taskID, leaseID, TaskLeased).
		Updates(updates)
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
}
```

- [ ] **Step 6: Update the `Store` interface**

In `pkg/store/store.go`:

```go
	// RecordTaskProgress stores a running task's current activity. Fenced by the
	// lease id so a daemon that lost the task cannot write to it.
	RecordTaskProgress(ctx context.Context, taskID, leaseID, phase, output string, phaseSince time.Time) (bool, error)
```

- [ ] **Step 7: Run the tests to verify they pass**

Run: `CGO_ENABLED=0 go test ./pkg/store/... -run TestRecordTaskProgress -v`
Expected: PASS.

- [ ] **Step 8: Run the full store package tests, vet, and build**

Run: `CGO_ENABLED=0 go vet ./pkg/store/... && CGO_ENABLED=0 go test ./pkg/store/... && CGO_ENABLED=0 go build ./...`
Expected: clean. (`go build ./...` will fail here because `ee/orchestrator/daemon_api.go` still calls the old 5-argument `RecordTaskProgress` — that's expected and fixed in Task 3. If you're running tasks strictly in order this is fine; if `go build ./...` is run standalone at this point, scope it to `CGO_ENABLED=0 go build ./pkg/...` instead.)

- [ ] **Step 9: Commit**

```bash
git add migrations/0034_task_progress_phase_since.up.sql migrations/0034_task_progress_phase_since.down.sql \
        pkg/store/queue_models.go pkg/store/queue_lifecycle.go pkg/store/queue_lifecycle_test.go pkg/store/store.go
git commit -m "feat(store): persist when a task's current progress phase started"
```

---

### Task 3: Wire `PhaseSince` through the daemon-facing progress endpoint

**Files:**
- Modify: `ee/orchestrator/daemon_api.go:811` (`handleDaemonProgress`)
- Modify: `ee/orchestrator/daemon_api_test.go`

**Interfaces:**
- Consumes: `daemon.ProgressReq.PhaseSince` (Task 1), `store.Store.RecordTaskProgress(..., phaseSince time.Time)` (Task 2).
- Produces: nothing new consumed by later tasks — this closes the daemon → DB half of the pipe. Task 4 reads back from the DB independently.

- [ ] **Step 1: Write the failing test**

In `ee/orchestrator/daemon_api_test.go`, add the progress route to `newSeamTestServer`'s mux:

```go
	mux.HandleFunc("/api/v1/daemon/register", s.handleDaemonRegister)
	mux.HandleFunc("/api/v1/daemon/heartbeat", s.handleDaemonHeartbeat)
	mux.HandleFunc("/api/v1/daemon/progress", s.handleDaemonProgress)
	mux.HandleFunc("/api/v1/daemon/result", s.handleDaemonResult)
```

Then add a new test, modeled on `TestDaemonSeam_EndToEnd`'s register→heartbeat→lease flow:

```go
// A progress report's PhaseSince must reach the queued_tasks row untouched —
// this is the field the dashboard uses to show how long the CURRENT phase has
// been running, distinct from when the daemon last reported at all.
func TestDaemonSeam_ProgressPersistsPhaseSince(t *testing.T) {
	ts, st := newSeamTestServer(t)
	ctx := context.Background()

	if err := st.DB().Create(&store.Organization{ID: "o1", Name: "Org One"}).Error; err != nil {
		t.Fatalf("create org: %v", err)
	}
	if err := st.EnqueueTask(ctx, &store.QueuedTask{
		ID: "job1-w0", OrgID: "o1", JobID: "job1", Status: store.TaskQueued,
		Spec: map[string]interface{}{"id": "job1-w0", "task": "fix the thing", "model": "sonnet"},
	}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	d := newDaemonKeys(t, ts.URL)
	token, err := st.CreateDaemonJoinToken(ctx, "o1", "", time.Hour)
	if err != nil {
		t.Fatalf("mint join token: %v", err)
	}
	if err := d.client.Register(ctx, daemon.RegisterReq{
		JoinToken: token, PubKey: d.encPubB64(), SignPubKey: d.signPubB64(),
	}); err != nil {
		t.Fatalf("register: %v", err)
	}

	res, err := d.client.Heartbeat(ctx, daemon.HeartbeatReq{
		PubKey: d.encPubB64(), SignPubKey: d.signPubB64(), Timestamp: time.Now().Unix(),
	})
	if err != nil || res == nil || len(res.Specs) != 1 {
		t.Fatalf("heartbeat: %v %+v", err, res)
	}

	since := time.Now().Add(-45 * time.Second).UTC().Truncate(time.Second)
	if err := d.client.ReportProgress(ctx, daemon.ProgressReq{
		TaskID: res.Specs[0].ID, LeaseID: res.LeaseID, SignPubKey: d.signPubB64(),
		Phase: "install: npm ci", PhaseSince: since,
	}); err != nil {
		t.Fatalf("report progress: %v", err)
	}

	var task store.QueuedTask
	if err := st.DB().First(&task, "id = ?", "job1-w0").Error; err != nil {
		t.Fatalf("reload task: %v", err)
	}
	if task.ProgressPhaseSince == nil || !task.ProgressPhaseSince.Equal(since) {
		t.Errorf("ProgressPhaseSince = %v, want %v", task.ProgressPhaseSince, since)
	}
	if task.ProgressPhase == nil || *task.ProgressPhase != "install: npm ci" {
		t.Errorf("ProgressPhase = %v, want %q", task.ProgressPhase, "install: npm ci")
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `CGO_ENABLED=0 go test ./ee/orchestrator/... -run TestDaemonSeam_ProgressPersistsPhaseSince -v`
Expected: FAIL to compile — `handleDaemonProgress` still calls `RecordTaskProgress` with 5 arguments.

- [ ] **Step 3: Pass `PhaseSince` through in `handleDaemonProgress`**

In `ee/orchestrator/daemon_api.go`, the existing call:

```go
	applied, err := s.storage.RecordTaskProgress(r.Context(), req.TaskID, req.LeaseID, req.Phase, summarize(req.OutputTail, 4000))
```

becomes:

```go
	applied, err := s.storage.RecordTaskProgress(r.Context(), req.TaskID, req.LeaseID, req.Phase, summarize(req.OutputTail, 4000), req.PhaseSince)
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `CGO_ENABLED=0 go test ./ee/orchestrator/... -run TestDaemonSeam_ProgressPersistsPhaseSince -v`
Expected: PASS.

- [ ] **Step 5: Run the full orchestrator test suite, vet, and build**

Run: `CGO_ENABLED=0 go vet ./ee/... && CGO_ENABLED=0 go test ./ee/... && CGO_ENABLED=0 go build ./...`
Expected: clean.

- [ ] **Step 6: Commit**

```bash
git add ee/orchestrator/daemon_api.go ee/orchestrator/daemon_api_test.go
git commit -m "feat(orchestrator): persist phase_since from daemon progress reports"
```

---

### Task 4: Surface `phase_since` on the job-progress API

**Files:**
- Modify: `ee/orchestrator/jobs_api.go:264-310` (`jobProgressTask`, `handleJobProgress`)
- Modify: `ee/orchestrator/jobs_api_test.go`

**Interfaces:**
- Consumes: `store.QueuedTask.ProgressPhaseSince` (Task 2).
- Produces: `jobProgressTask.PhaseSince *time.Time` (JSON `phase_since`, `omitempty`) — this is the field Task 5's frontend change reads.

- [ ] **Step 1: Write the failing test**

Add to `ee/orchestrator/jobs_api_test.go`:

```go
// handleJobProgress must carry phase_since through to the dashboard, so a
// running task's live indicator can show how long its current phase has been
// going — the same information the daemon reported, not re-derived.
func TestHandleJobProgress_IncludesPhaseSince(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(
		&store.Organization{}, &store.OrgLimits{}, &store.QueuedTask{},
		&store.Credential{}, &store.Daemon{}, &store.DaemonJoinToken{}, &store.Job{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	st := store.NewPostgresStore(db)
	srv := &Server{db: db, storage: st}

	since := time.Now().Add(-2 * time.Minute).UTC().Truncate(time.Second)
	phase := "install: npm ci"
	task := store.QueuedTask{
		ID: "j1-t1", OrgID: "org-1", JobID: "j1", Status: store.TaskLeased,
		ProgressPhase: &phase, ProgressPhaseSince: &since,
	}
	if err := st.DB().Create(&task).Error; err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/jobs/j1/progress", nil)
	rr := httptest.NewRecorder()
	srv.handleJobProgress(rr, req, "org-1", "j1")

	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d. body: %s", rr.Code, rr.Body.String())
	}
	var resp struct {
		Tasks []jobProgressTask `json:"tasks"`
	}
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Tasks) != 1 {
		t.Fatalf("tasks len: got %d, want 1", len(resp.Tasks))
	}
	if resp.Tasks[0].PhaseSince == nil || !resp.Tasks[0].PhaseSince.Equal(since) {
		t.Errorf("PhaseSince = %v, want %v", resp.Tasks[0].PhaseSince, since)
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `CGO_ENABLED=0 go test ./ee/orchestrator/... -run TestHandleJobProgress_IncludesPhaseSince -v`
Expected: FAIL — `jobProgressTask` has no `PhaseSince` field yet, so it decodes as absent / test assertion fails (or compile error if referenced directly elsewhere — here it's read from the decoded JSON struct which does compile, so this will be a runtime assertion failure: `PhaseSince == nil`).

- [ ] **Step 3: Add the field and populate it**

In `ee/orchestrator/jobs_api.go`, `jobProgressTask`:

```go
type jobProgressTask struct {
	TaskID string          `json:"task_id"`
	Status string          `json:"status"`
	Model  string          `json:"actor_model,omitempty"`
	Steps  []ver.TaskEvent `json:"steps"`
	Phase  string          `json:"phase,omitempty"`
	Output string          `json:"output_tail,omitempty"`
	// ProgressAt is when the daemon last reported. A timestamp that stops
	// advancing is how a hung run distinguishes itself from a slow one.
	ProgressAt *time.Time `json:"progress_at,omitempty"`
	// PhaseSince is when the current Phase started — how long this step has
	// actually been running, distinct from ProgressAt (whether the feed is
	// still alive at all).
	PhaseSince *time.Time `json:"phase_since,omitempty"`
}
```

In `handleJobProgress`, alongside the existing `p.ProgressAt = t.ProgressAt`:

```go
		if t.ProgressPhase != nil {
			p.Phase = *t.ProgressPhase
		}
		if t.ProgressOutput != nil {
			p.Output = *t.ProgressOutput
		}
		p.ProgressAt = t.ProgressAt
		p.PhaseSince = t.ProgressPhaseSince
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `CGO_ENABLED=0 go test ./ee/orchestrator/... -run TestHandleJobProgress_IncludesPhaseSince -v`
Expected: PASS.

- [ ] **Step 5: Run the full orchestrator test suite, vet, and build**

Run: `CGO_ENABLED=0 go vet ./ee/... && CGO_ENABLED=0 go test ./ee/... && CGO_ENABLED=0 go build ./...`
Expected: clean.

- [ ] **Step 6: Commit**

```bash
git add ee/orchestrator/jobs_api.go ee/orchestrator/jobs_api_test.go
git commit -m "feat(orchestrator): expose phase_since on the job progress API"
```

---

### Task 5: Render elapsed time in the live "now" line

**Files:**
- Create: `frontend/src/lib/progressTime.ts`
- Create: `frontend/src/lib/progressTime.test.ts`
- Modify: `frontend/src/lib/api.ts:737-747` (`JobProgressTask`)
- Modify: `frontend/src/components/LiveRun.tsx`

**Interfaces:**
- Consumes: `JobProgressTask.phase_since?: string` (Task 4's JSON field).
- Produces: `elapsedSince(phaseSince?: string): number | null` — seconds since the phase began, or `null` when absent. Exported for the test; imported by `LiveRun.tsx`.

- [ ] **Step 1: Write the failing test**

`frontend/src/lib/progressTime.test.ts`:

```ts
import { describe, it } from "node:test";
import assert from "node:assert";
import { elapsedSince } from "./progressTime.ts";

describe("elapsedSince", () => {
  it("returns null when there is no phase_since", () => {
    assert.strictEqual(elapsedSince(undefined), null);
  });

  it("returns null for an unparseable timestamp", () => {
    assert.strictEqual(elapsedSince("not-a-date"), null);
  });

  it("returns whole seconds elapsed since the timestamp", () => {
    const since = new Date(Date.now() - 90_000).toISOString();
    const got = elapsedSince(since);
    assert.ok(got !== null && got >= 89 && got <= 91, `got ${got}, want ~90`);
  });

  it("never returns negative — a clock skewed forward reads as just-started", () => {
    const future = new Date(Date.now() + 5_000).toISOString();
    assert.strictEqual(elapsedSince(future), 0);
  });
});
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd frontend && npm test -- --test-name-pattern=elapsedSince`
Expected: FAIL — `./progressTime.ts` does not exist yet.

- [ ] **Step 3: Implement `elapsedSince`**

`frontend/src/lib/progressTime.ts`:

```ts
/**
 * Seconds since a phase began, or null when there is nothing to measure.
 *
 * This is a different question from LiveRun's own `staleness()`: staleness
 * asks whether the feed is still arriving (time since the daemon last
 * reported anything); this asks how long the CURRENT phase has taken, which
 * keeps advancing even while the feed is healthy and reporting the same
 * phase every three seconds. A four-minute Architect plan and a
 * just-started one otherwise render identically.
 */
export function elapsedSince(phaseSince?: string): number | null {
  if (!phaseSince) return null;
  const t = Date.parse(phaseSince);
  if (Number.isNaN(t)) return null;
  return Math.max(0, Math.round((Date.now() - t) / 1000));
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `cd frontend && npm test -- --test-name-pattern=elapsedSince`
Expected: PASS.

- [ ] **Step 5: Add the field to `JobProgressTask` and render it in `LiveRun.tsx`**

In `frontend/src/lib/api.ts`:

```ts
export interface JobProgressTask {
  task_id: string;
  status: string;
  actor_model?: string;
  steps: RecordStep[];
  phase?: string;
  output_tail?: string;
  progress_at?: string;
  // When the current `phase` started — how long this step has actually been
  // running. Distinct from progress_at, which is when the daemon last said
  // anything at all.
  phase_since?: string;
}
```

In `frontend/src/components/LiveRun.tsx`, import and use it. Add the import:

```tsx
import { elapsedSince } from "@/lib/progressTime";
```

In the `running.map` block, alongside the existing `since`:

```tsx
      {running.map(t => {
        const { kind, command } = splitPhase(t.phase ?? "");
        const since = staleness(t.progress_at);
        const elapsed = elapsedSince(t.phase_since);
        return (
          <div key={t.task_id} className="rounded-lg bg-black/30 border border-white/5 p-2.5 flex flex-col gap-2">
            <div className="flex items-center gap-2 text-xs">
              <ThinkingOrb
                state={orbStateForPhase(t.phase)}
                size={20}
                className="shrink-0"
                aria-label={`${kind || "working"}${command ? `: ${command}` : ""}`}
              />
              <span className="text-zinc-300">{kind || "working"}</span>
              {command && <code className="text-[11px] text-zinc-500 font-mono truncate">{command}</code>}
              {/* How long the CURRENT phase has taken — distinct from the
                  staleness warning below, which is about whether the feed
                  itself is still arriving. */}
              {elapsed !== null && (
                <span className="text-[11px] text-zinc-500 font-mono tabular-nums shrink-0">
                  {formatElapsed(elapsed)}
                </span>
              )}
              {since !== null && since > 30 && (
                <span className="ml-auto text-[11px] text-amber-400/80 shrink-0">
                  no update for {since}s
                </span>
              )}
            </div>
            {t.output_tail ? <OutputTail text={t.output_tail} /> : null}
          </div>
        );
      })}
```

Add a small formatter above the `LiveRun` export, next to `staleness`:

```tsx
/** Seconds as "12s" or "4m32s" — short enough for an inline badge. */
function formatElapsed(seconds: number): string {
  if (seconds < 60) return `${seconds}s`;
  const m = Math.floor(seconds / 60);
  const s = seconds % 60;
  return `${m}m${s.toString().padStart(2, "0")}s`;
}
```

- [ ] **Step 6: Run the full frontend test suite**

Run: `cd frontend && npm test`
Expected: PASS, including the pre-existing `orbState.test.ts` suite.

- [ ] **Step 7: Run the frontend build/typecheck**

Run: `cd frontend && npm run build`
Expected: clean (this catches any TypeScript mismatch between `LiveRun.tsx` and the new `JobProgressTask.phase_since` field).

- [ ] **Step 8: Commit**

```bash
git add frontend/src/lib/progressTime.ts frontend/src/lib/progressTime.test.ts \
        frontend/src/lib/api.ts frontend/src/components/LiveRun.tsx
git commit -m "feat(frontend): show elapsed time on the live progress indicator"
```

---

### Task 6: Give the setup phase (clone, initial install) a live and historical signal

**Files:**
- Modify: `pkg/daemon/daemon.go:460-599` (`executeTask`)
- Create: `pkg/daemon/setup_phase_test.go`
- Modify: `frontend/src/components/RunTimeline.tsx:74-85` (`PHASE_LABEL`)

**Interfaces:**
- Consumes: `progressReporter.setActivity`, `progressReporter.add` (existing, unchanged signatures).
- Produces: `reportSetupPhase(prog *progressReporter, phase, activity, fallbackDetail string, fn func() error) error` in `pkg/daemon/daemon.go` — a small helper other setup-style call sites could reuse later, but nothing in this plan depends on it beyond the two call sites added here.

This is the one part of the plan that changes `executeTask` itself, which is
not independently unit-tested end-to-end (it needs real git/docker). The
approach: extract the "time it, report it live, record it in history" logic
into a small helper that takes an injectable `func() error`, so the logic is
fully testable without touching git or docker, and the two call sites become
thin, low-risk wiring.

- [ ] **Step 1: Write the failing tests for the helper**

`pkg/daemon/setup_phase_test.go`:

```go
package daemon

import (
	"errors"
	"testing"
	"time"
)

// A successful step reports itself as the live activity before it runs and
// records one durable Step-0 event with outcome "ok" once it finishes.
func TestReportSetupPhase_Success(t *testing.T) {
	p := &progressReporter{}

	err := reportSetupPhase(p, "install", "install: npm ci", "npm ci", func() error {
		time.Sleep(time.Millisecond)
		return nil
	})
	if err != nil {
		t.Fatalf("reportSetupPhase: %v", err)
	}

	_, phase, _, phaseSince, _ := p.pending()
	if phase != "install: npm ci" {
		t.Errorf("live phase = %q, want %q", phase, "install: npm ci")
	}
	if phaseSince.IsZero() {
		t.Error("phaseSince should have been set")
	}

	events := p.all()
	if len(events) != 1 {
		t.Fatalf("expected 1 durable event, got %d: %+v", len(events), events)
	}
	if events[0].Phase != "install" || events[0].Outcome != "ok" || events[0].Step != 0 {
		t.Errorf("unexpected event: %+v", events[0])
	}
	if events[0].Detail != "npm ci" {
		t.Errorf("Detail = %q, want the fallback %q", events[0].Detail, "npm ci")
	}
	if events[0].DurationMs < 1 {
		t.Errorf("DurationMs = %d, want > 0", events[0].DurationMs)
	}
}

// A failing step records outcome "error" with the failure's own message as
// Detail, not the generic fallback — the fallback exists for the success
// case, where there is nothing more specific to say than the command itself.
func TestReportSetupPhase_Failure(t *testing.T) {
	p := &progressReporter{}

	err := reportSetupPhase(p, "clone", "clone: https://example/repo", "https://example/repo", func() error {
		return errors.New("could not read Username for https://example/repo")
	})
	if err == nil {
		t.Fatal("expected the underlying error to propagate")
	}

	events := p.all()
	if len(events) != 1 {
		t.Fatalf("expected 1 durable event, got %d", len(events))
	}
	if events[0].Outcome != "error" {
		t.Errorf("Outcome = %q, want error", events[0].Outcome)
	}
	if events[0].Detail != "could not read Username for https://example/repo" {
		t.Errorf("Detail = %q, want the error message", events[0].Detail)
	}
}

// A nil reporter (the same convention every other progressReporter method
// follows) must not panic — some callers run without one.
func TestReportSetupPhase_NilReporterIsSafe(t *testing.T) {
	var p *progressReporter
	err := reportSetupPhase(p, "install", "install: npm ci", "npm ci", func() error { return nil })
	if err != nil {
		t.Fatalf("reportSetupPhase with nil reporter: %v", err)
	}
}
```

- [ ] **Step 2: Run the tests to verify they fail**

Run: `CGO_ENABLED=0 go test ./pkg/daemon/... -run TestReportSetupPhase -v`
Expected: FAIL to compile — `reportSetupPhase` does not exist yet.

- [ ] **Step 3: Implement `reportSetupPhase` in `pkg/daemon/daemon.go`**

Add near `installDependencies` (the function it will wrap one call site around):

```go
// reportSetupPhase runs fn, reporting it as the live activity before it starts
// and recording one durable Step-0 event with its outcome and duration once it
// finishes.
//
// Setup (clone, initial dependency install) happens before session.Runner
// exists, so it is the one part of a run with no OnEvent/OnActivity of its
// own to hook — this gives it the same live-plus-historical signal every
// later phase already has via those. fallbackDetail is what a SUCCESSFUL run
// records, since a clean install has nothing more informative to say than the
// command itself; a failing fn's own error message is used instead, since
// that names what actually went wrong.
func reportSetupPhase(prog *progressReporter, phase, activity, fallbackDetail string, fn func() error) error {
	prog.setActivity(activity, "")
	start := time.Now()
	err := fn()
	outcome, detail := "ok", fallbackDetail
	if err != nil {
		outcome, detail = "error", err.Error()
	}
	prog.add(ver.TaskEvent{
		Step:       0,
		Phase:      phase,
		Outcome:    outcome,
		Detail:     detail,
		DurationMs: time.Since(start).Milliseconds(),
	})
	return err
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `CGO_ENABLED=0 go test ./pkg/daemon/... -run TestReportSetupPhase -v`
Expected: PASS.

- [ ] **Step 5: Wire the clone call site**

In `pkg/daemon/daemon.go`, `executeTask`, the existing block:

```go
		cloneToken, err := d.resolveGitToken(ctx, spec.ID, leaseID, creds)
		if err != nil {
			log.Printf("Failed to resolve git credential for task %s: %v", spec.ID, err)
			return taskResult{detail: truncateDetail(err.Error())}
		}
		if err := d.gitCache.GetJobWorktree(ctx, spec.RepoURL, spec.Ref, jobBranch, worktreePath,
			gitcache.WithToken(cloneToken)); err != nil {
			log.Printf("Failed to provision worktree for task %s: %v", spec.ID, err)
			return taskResult{detail: "failed to provision worktree"}
		}
```

becomes:

```go
		cloneToken, err := d.resolveGitToken(ctx, spec.ID, leaseID, creds)
		if err != nil {
			log.Printf("Failed to resolve git credential for task %s: %v", spec.ID, err)
			return taskResult{detail: truncateDetail(err.Error())}
		}
		if err := reportSetupPhase(prog, "clone", "clone: "+spec.RepoURL, spec.RepoURL, func() error {
			return d.gitCache.GetJobWorktree(ctx, spec.RepoURL, spec.Ref, jobBranch, worktreePath,
				gitcache.WithToken(cloneToken))
		}); err != nil {
			log.Printf("Failed to provision worktree for task %s: %v", spec.ID, err)
			return taskResult{detail: "failed to provision worktree"}
		}
```

- [ ] **Step 6: Wire the initial-install call site**

The existing block:

```go
	// Phase A: fetch the repository's declared dependencies, with network and
	// without credentials. Runs once, before the loop, so the Actor never sees
	// a missing-module error it cannot fix by editing code. See deps.go for why
	// this is separated from verification rather than solved by relaxing
	// --network none.
	if step := inferInstallStep(worktreePath); step != nil {
		if detail, ok := d.installDependencies(taskCtx, worktreePath, sandboxCfg, step, spec.ID, cacheEnv); !ok {
			return taskResult{detail: detail}
		}
	}
```

becomes:

```go
	// Phase A: fetch the repository's declared dependencies, with network and
	// without credentials. Runs once, before the loop, so the Actor never sees
	// a missing-module error it cannot fix by editing code. See deps.go for why
	// this is separated from verification rather than solved by relaxing
	// --network none.
	if step := inferInstallStep(worktreePath); step != nil {
		var installDetail string
		err := reportSetupPhase(prog, "install", "install: "+step.Command, step.Command, func() error {
			var ok bool
			installDetail, ok = d.installDependencies(taskCtx, worktreePath, sandboxCfg, step, spec.ID, cacheEnv)
			if !ok {
				return errors.New(installDetail)
			}
			return nil
		})
		if err != nil {
			return taskResult{detail: installDetail}
		}
	}
```

- [ ] **Step 7: Run the daemon package tests, vet, and build**

Run: `CGO_ENABLED=0 go vet ./pkg/daemon/... && CGO_ENABLED=0 go test ./pkg/daemon/... && CGO_ENABLED=0 go build ./...`
Expected: clean. In particular, `TestInstallPhase_PassesNoCredentials` in `pkg/daemon/install_phase_test.go` (calls `installDependencies` directly, not through `reportSetupPhase`) must still pass unchanged — it is not affected by this wiring, and its pass confirms Phase A's no-credentials property is untouched.

- [ ] **Step 8: Add the two new phase labels to the frontend timeline**

In `frontend/src/components/RunTimeline.tsx`, `PHASE_LABEL`:

```ts
const PHASE_LABEL: Record<string, string> = {
  initial_test: "Baseline check",
  actor: "Actor",
  critic: "Critic",
  test: "Test",
  clone: "Cloning repository",
  install: "Installing dependencies",
  // Session mode emits these raw (pkg/daemon/session_run.go sessionPhase), so
  // without them a session run showed bare snake_case rows.
  round_start: "Round started",
  session_end: "Session ended",
  implementer: "Implementer",
  compaction: "Compacted context",
};
```

(`orbStateForPhase` in `frontend/src/lib/orbState.ts` already maps `clone` → `connecting` and `install` → `working`, per its own docstring anticipating exactly these two progress phases — no change needed there.)

- [ ] **Step 9: Run the frontend test suite and build**

Run: `cd frontend && npm test && npm run build`
Expected: PASS / clean.

- [ ] **Step 10: Commit**

```bash
git add pkg/daemon/daemon.go pkg/daemon/setup_phase_test.go frontend/src/components/RunTimeline.tsx
git commit -m "feat(daemon): give the setup phase (clone, initial install) live and historical visibility"
```

---

## Final Verification

- [ ] Run the full mandatory pre-commit suite from CLAUDE.md:

```bash
gofmt -l cmd/ pkg/ ee/
CGO_ENABLED=0 go vet ./...
CGO_ENABLED=0 go test ./pkg/...
CGO_ENABLED=0 go build ./...
```

All four must be clean/pass before this branch is considered done.

- [ ] Run the orchestrator test suite too, since Tasks 3–4 touch `ee/`:

```bash
CGO_ENABLED=0 go test ./ee/...
```

- [ ] Run the frontend suite one more time end to end:

```bash
cd frontend && npm test && npm run build
```

- [ ] Manual smoke check (per CLAUDE.md's UI guidance — type checking and test
  suites verify correctness, not that the feature reads well): submit a real
  task against a repo with a slow install, and confirm in the dashboard that
  (a) the orb shows "Cloning repository" / "Installing dependencies" instead
  of a generic "working" blob during setup, (b) the finished run's timeline
  has rows for both, and (c) the live indicator's elapsed badge advances
  while the Architect is planning.
