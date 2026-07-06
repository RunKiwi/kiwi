# Design: Crash-Safe Restart Recovery + Idempotent Submission

**Date:** 2026-07-06
**Status:** Approved (pending spec review)
**Author:** Kiwi cofounders (pairing session)

## Goal

Make the daemon durable across restarts and safe against duplicate submissions.
Today task *records* persist in SQLite, but task *execution* is in-memory only:
a restart orphans RUNNING/PAUSED tasks as permanent zombies (no goroutine, never
completes), and `POST /tasks` uses random IDs with no dedupe, so a retried submit
creates a second task, sandbox, and LLM bill.

## Scope

**In scope**
- Extract the inline task-run goroutine into a reusable `launchTask` method.
- Boot recovery: on startup, re-launch interrupted tasks whose sandbox survived,
  or mark them FAILED if the sandbox is gone.
- Idempotent submission via a client-supplied `Idempotency-Key` header.
- Client support: optional `-idempotency-key` flag.

**Out of scope (deliberately)**
- Multi-daemon / distributed locking.
- Durable workspace storage (object store) — the on-disk sandbox check covers
  process restarts, which is the actual gap.
- Retrying the exact in-flight LLM call — recovery restarts the loop from the
  current file state, which is safe because the loop is convergent (tests gate).

## Current state (verified this session)

- `cmd/kiwid/main.go`: `InitDB` → `NewServer` → `Start`; no reconciliation.
- `pkg/orchestrator/server.go`: the run logic (logBuf, engine selection,
  callbacks, 2-phase goroutine, `RunTask` at ~:269, final status) is inline in
  `handleTasks`. `generateTaskID()` returns random hex. `TaskState` has no
  idempotency column.
- `Server.Start()` only wires routes.

## Architecture

### 1. `launchTask` extraction (`server.go`)

Move the entire task-run goroutine body out of `handleTasks` into:

```go
// launchTask runs the orchestration loop for an already-persisted task whose
// sandbox is already populated on disk. Safe to call from submission and from
// boot recovery.
func (s *Server) launchTask(taskID, sandboxPath, task, file, testCmd string)
```

It performs exactly what the current inline goroutine does: create `logBuf`,
select provider (`KIWI_LLM_PROVIDER`), wire `StateCallback`/`CostCallback`, set
`MaxBudget` and the context timeout, start the 500ms log-syncer, run
`engine.RunTask`, and write the final status + `output.zip` on success.

`handleTasks` (POST), after creating the DB row and unzipping the upload, calls
`s.launchFn(taskID, tempSandbox, task, file, testCmd)`.

### 2. Injectable launch for testability (`server.go`)

```go
type Server struct {
    db       *gorm.DB
    launchFn func(taskID, sandboxPath, task, file, testCmd string)
}
```

`NewServer` sets `launchFn = s.launchTask` by default. Tests override it with a
recorder to assert recovery decisions without running the engine.

### 3. Boot recovery (`server.go` + `cmd/kiwid`)

```go
// RecoverTasks re-launches interrupted tasks after a restart, or fails those
// whose sandbox no longer exists on disk.
func (s *Server) RecoverTasks()
```

Logic:
- Query rows where `status IN ('RUNNING','PAUSED')`.
- For each row:
  - If `row.SandboxPath != ""` and `os.Stat(row.SandboxPath)` succeeds:
    - `tunnel.GlobalRegistry.Register(row.ID)` (so the client can reconnect).
    - `s.launchFn(row.ID, row.SandboxPath, row.Task, row.FilePath, row.TestCmd)`.
    - Append a `[Recovery] Re-launched after daemon restart` log line.
  - Else:
    - `Updates({status: "FAILED", logs: logs + "[Recovery] Interrupted by daemon restart; sandbox unavailable"})`.

`cmd/kiwid/main.go` calls `server.RecoverTasks()` after `NewServer(db)` and
before `Start()`.

Recovery decision is factored into a pure helper for unit testing:
```go
// recoverAction returns "relaunch" if the sandbox is usable, else "fail".
func recoverAction(sandboxPath string) string
```

### 4. Idempotent submission (`server.go` + `db.go`)

- Add to `TaskState`: `IdempotencyKey string \`json:"-" gorm:"index"\``. AutoMigrate
  adds the column to existing databases.
- `handleTasks` POST, before creating the sandbox:
  - Read `key := r.Header.Get("Idempotency-Key")`.
  - If `key != ""`: `SELECT ... WHERE idempotency_key = ? LIMIT 1`. If found,
    respond `200 {"task_id": existing.ID, "status": existing.Status}` and return —
    no sandbox, no launch.
  - Else create the row with `IdempotencyKey: key` and proceed.
- Helper: `func findByIdempotencyKey(db *gorm.DB, key string) (*TaskState, bool)`.
- Known limitation (documented, accepted): two simultaneous submits with the same
  key can both miss and create two rows — acceptable for a single low-concurrency
  daemon; revisit with a unique index + insert-conflict handling if it matters.

### 5. Client (`pkg/client` + `cmd/kiwi`)

- Add field `IdempotencyKey string` to `client.Client`. In `SubmitTask`, if
  non-empty, set `req.Header.Set("Idempotency-Key", c.IdempotencyKey)`. Method
  signature unchanged; existing tests unaffected (field defaults to "").
- Add `-idempotency-key` flag to `cmd/kiwi`; set `c.IdempotencyKey` from it.

## Data flow

```
kiwid boot:
  InitDB → NewServer(db) → RecoverTasks()
     for each RUNNING/PAUSED row:
        sandbox on disk?  yes → register tunnel + launchTask (resume)
                          no  → mark FAILED (interrupted)
  → Start()

POST /tasks (Idempotency-Key: K):
  K set and row with K exists? → return existing {task_id,status}, stop
  else → create row (key=K) → unzip → launchTask
```

## Error handling

- `os.Stat` error other than not-exist (e.g. permission) → treat as unusable →
  FAIL the task (conservative; don't relaunch into a broken sandbox).
- Recovery of many tasks: each row handled independently; one failure doesn't
  abort the rest. Recovery runs synchronously before `Start()` but each
  `launchTask` spawns its own goroutine, so boot is not blocked by task runtime.
- Empty `Idempotency-Key` → current behavior (always create), so nothing breaks
  for existing clients.

## Testing

- `recoverAction`: existing dir → "relaunch"; missing dir → "fail"; empty path →
  "fail".
- `RecoverTasks` (temp SQLite, `launchFn` recorder):
  - RUNNING row + existing sandbox dir → `launchFn` called with the row's fields;
    status left for the (stubbed) launch to manage.
  - RUNNING row + missing sandbox → status becomes FAILED, `launchFn` NOT called.
  - SUCCESS/FAILED rows → ignored.
- `findByIdempotencyKey`: hit, miss, empty-key.
- Idempotency integration (httptest against `s.handleTasks`, `launchFn` stubbed,
  temp SQLite): two multipart POSTs with the same key → one row created, second
  returns the same `task_id`; different keys → two rows.
- Client: `SubmitTask` sends `Idempotency-Key` when the field is set; omits it
  when empty (assert via httptest handler inspecting the header).

## Build / verification

- `CGO_ENABLED=0 go test ./pkg/...` green.
- Build both binaries (`./cmd/...` path form, external linkmode + codesign).
- Manual: start daemon, submit a task, `kill -9` the daemon mid-run, restart, and
  confirm the recovery log line and that the task is no longer a zombie (relaunch
  if the temp sandbox survived, else FAILED). Submit twice with the same
  `-idempotency-key` and confirm one task.

## Risks / notes

- Re-launching restarts the loop from the current on-disk file state; because the
  loop is convergent and tests are the gate, a partially-applied edit is simply
  re-evaluated — no corruption risk.
- `NewServer` signature is unchanged externally (still `NewServer(db)`), so
  `cmd/kiwid` only adds the `RecoverTasks()` call.
