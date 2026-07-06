# Crash-Safe Restart Recovery + Idempotency Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the daemon durable across restarts (re-launch interrupted tasks whose sandbox survived, else mark them FAILED) and idempotent against duplicate submissions (client `Idempotency-Key` header).

**Architecture:** Extract the inline task-run goroutine into a reusable, injectable `launchTask`; add a boot-time `RecoverTasks` sweep over RUNNING/PAUSED rows; add an `IdempotencyKey` column and header-based dedupe in `POST /tasks`; thread an optional key through the client.

**Tech Stack:** Go 1.24, GORM/SQLite, stdlib `net/http`/`os`, existing `pkg/orchestrator`, `pkg/client`, `pkg/tunnel`, `pkg/sandbox`.

## Global Constraints

- Module path: `github.com/ibreakthecloud/kiwi`.
- Build/sign per CLAUDE.md using the `./cmd/...` path form (Go ≥ 1.24).
- `NewServer(db)` external signature unchanged; `cmd/kiwid` adds only a `RecoverTasks()` call.
- Existing `Provider` interface and `client.SubmitTask` signature unchanged.
- Tests run `CGO_ENABLED=0 go test ./pkg/...`; tests never hit the network or run the real engine (stub `launchFn`).
- Recovery must not block boot: each `launchTask` spawns its own goroutine.
- Empty `Idempotency-Key` preserves current always-create behavior.

---

### Task 1: `IdempotencyKey` column + `findByIdempotencyKey`

**Files:**
- Modify: `pkg/orchestrator/server.go` (add field to `TaskState`)
- Create: `pkg/orchestrator/idempotency.go`
- Test: `pkg/orchestrator/idempotency_test.go`

**Interfaces:**
- Consumes: `TaskState`, `gorm.DB`.
- Produces: `func findByIdempotencyKey(db *gorm.DB, key string) (*TaskState, bool)`; new field `TaskState.IdempotencyKey string`.

- [ ] **Step 1: Add the column**

In `pkg/orchestrator/server.go`, add to the `TaskState` struct (after `Cost`):

```go
	Cost           float64   `json:"cost"`
	IdempotencyKey string    `json:"-" gorm:"index"`
```

- [ ] **Step 2: Write the failing test**

```go
// pkg/orchestrator/idempotency_test.go
package orchestrator

import (
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func newTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&TaskState{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	// clean slate for the shared in-memory db
	db.Exec("DELETE FROM task_states")
	return db
}

func TestFindByIdempotencyKey(t *testing.T) {
	db := newTestDB(t)
	db.Create(&TaskState{ID: "t1", IdempotencyKey: "key-abc", Status: "RUNNING"})

	got, ok := findByIdempotencyKey(db, "key-abc")
	if !ok || got.ID != "t1" {
		t.Fatalf("hit: ok=%v got=%+v", ok, got)
	}
	if _, ok := findByIdempotencyKey(db, "nope"); ok {
		t.Errorf("miss should be false")
	}
	if _, ok := findByIdempotencyKey(db, ""); ok {
		t.Errorf("empty key should never match")
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `CGO_ENABLED=0 go test ./pkg/orchestrator/ -run TestFindByIdempotencyKey -v`
Expected: FAIL to compile — `undefined: findByIdempotencyKey`.

- [ ] **Step 4: Write minimal implementation**

```go
// pkg/orchestrator/idempotency.go
package orchestrator

import "gorm.io/gorm"

// findByIdempotencyKey returns an existing task for the given key, if any.
// An empty key never matches (idempotency is opt-in).
func findByIdempotencyKey(db *gorm.DB, key string) (*TaskState, bool) {
	if key == "" {
		return nil, false
	}
	var t TaskState
	if err := db.Where("idempotency_key = ?", key).First(&t).Error; err != nil {
		return nil, false
	}
	return &t, true
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `CGO_ENABLED=0 go test ./pkg/orchestrator/ -run TestFindByIdempotencyKey -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add pkg/orchestrator/server.go pkg/orchestrator/idempotency.go pkg/orchestrator/idempotency_test.go
git commit -m "feat(orchestrator): add IdempotencyKey column and lookup helper"
```

---

### Task 2: Extract `launchTask` + injectable `launchFn`

**Files:**
- Modify: `pkg/orchestrator/server.go` (`Server` struct, `NewServer`, `handleTasks`; new `launchTask`)

**Interfaces:**
- Consumes: `Engine`, `NewEngine`, `provider.*`, `sandbox.ZipDir`, `tunnel`, `taskTimeout`, `maxBudget`.
- Produces: `func (s *Server) launchTask(taskID, sandboxPath, task, file, testCmd string)`; `Server.launchFn` field.

- [ ] **Step 1: Add the `launchFn` field and default it**

Replace the `Server` struct and `NewServer`:

```go
type Server struct {
	db       *gorm.DB
	launchFn func(taskID, sandboxPath, task, file, testCmd string)
}

func NewServer(db *gorm.DB) *Server {
	s := &Server{db: db}
	s.launchFn = s.launchTask
	return s
}
```

- [ ] **Step 2: Add `launchTask` (extracted from the inline goroutine, sandbox-parameterized, log-seeded)**

Add this method to `pkg/orchestrator/server.go`:

```go
// launchTask runs the orchestration loop in the background for an
// already-persisted task whose sandbox is populated on disk. Used by both
// submission and boot recovery. It seeds the log buffer from the existing row so
// recovered tasks keep their prior logs.
func (s *Server) launchTask(taskID, sandboxPath, task, file, testCmd string) {
	go func() {
		logBuf := new(bytes.Buffer)
		var existing TaskState
		if err := s.db.First(&existing, "id = ?", taskID).Error; err == nil {
			logBuf.WriteString(existing.Logs)
		}

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
		engine.LogOut = logBuf
		engine.StateCallback = func(newStatus string) {
			s.db.Model(&TaskState{}).Where("id = ?", taskID).Update("status", newStatus)
		}
		engine.CostCallback = func(amount float64) {
			s.db.Model(&TaskState{}).Where("id = ?", taskID).Update("cost", gorm.Expr("cost + ?", amount))
		}

		ctx, cancel := context.WithTimeout(context.Background(), taskTimeout())
		defer cancel()

		// Periodic background log synchronizer to SQLite row (every 500ms)
		go func() {
			ticker := time.NewTicker(500 * time.Millisecond)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					s.db.Model(&TaskState{}).Where("id = ?", taskID).Update("logs", logBuf.String())
					return
				case <-ticker.C:
					s.db.Model(&TaskState{}).Where("id = ?", taskID).Update("logs", logBuf.String())
				}
			}
		}()

		absFilePath := filepath.Join(sandboxPath, file)
		fmt.Fprintf(logBuf, "[Orchestrator] Running task in sandbox: %s\n", sandboxPath)

		err := engine.RunTask(ctx, taskID, sandboxPath, task, absFilePath, testCmd)

		finalStatus := "SUCCESS"
		if err != nil {
			finalStatus = "FAILED"
			fmt.Fprintf(logBuf, "\n[Execution Failure]: %v\n", err)
		} else {
			fmt.Fprintln(logBuf, "\n[Execution Success]: Task completed successfully.")
			fixedZip, zerr := sandbox.ZipDir(sandboxPath)
			if zerr == nil {
				_ = os.WriteFile(filepath.Join(sandboxPath, "output.zip"), fixedZip, 0644)
			}
		}

		s.db.Model(&TaskState{}).Where("id = ?", taskID).Updates(map[string]interface{}{
			"status": finalStatus,
			"logs":   logBuf.String(),
		})
	}()
}
```

- [ ] **Step 3: Replace the inline goroutine in `handleTasks` with a `launchFn` call**

In `handleTasks`, delete the entire `// Launch loop orchestration in the background` block (the `go func() { ... }()` spanning the engine setup through the final status update) and replace it with:

```go
	// Launch loop orchestration in the background (injectable for tests).
	s.launchFn(taskID, tempSandbox, task, file, testCmd)
```

- [ ] **Step 4: Build to verify the refactor compiles**

Run: `CGO_ENABLED=0 go build ./...`
Expected: no output (success).

- [ ] **Step 5: Verify existing behavior via the full suite**

Run: `CGO_ENABLED=0 go test ./pkg/... 2>&1 | tail -8`
Expected: all packages PASS (refactor changes no behavior; idempotency test from Task 1 still green).

- [ ] **Step 6: Commit**

```bash
git add pkg/orchestrator/server.go
git commit -m "refactor(orchestrator): extract injectable launchTask from handleTasks"
```

---

### Task 3: `recoverAction` + `RecoverTasks` + daemon wiring

**Files:**
- Create: `pkg/orchestrator/recovery.go`
- Test: `pkg/orchestrator/recovery_test.go`
- Modify: `cmd/kiwid/main.go`

**Interfaces:**
- Consumes: `Server` (with `db`, `launchFn` from Task 2), `TaskState`, `tunnel.GlobalRegistry`.
- Produces: `func recoverAction(sandboxPath string) string`; `func (s *Server) RecoverTasks()`.

- [ ] **Step 1: Write the failing tests**

```go
// pkg/orchestrator/recovery_test.go
package orchestrator

import (
	"os"
	"testing"
)

func TestRecoverAction(t *testing.T) {
	dir := t.TempDir()
	if got := recoverAction(dir); got != "relaunch" {
		t.Errorf("existing dir: got %q want relaunch", got)
	}
	if got := recoverAction(dir + "/missing"); got != "fail" {
		t.Errorf("missing dir: got %q want fail", got)
	}
	if got := recoverAction(""); got != "fail" {
		t.Errorf("empty path: got %q want fail", got)
	}
}

func TestRecoverTasks(t *testing.T) {
	db := newTestDB(t)
	liveDir := t.TempDir() // exists → relaunch
	db.Create(&TaskState{ID: "live", Status: "RUNNING", SandboxPath: liveDir, Task: "t", FilePath: "a.go", TestCmd: "go test"})
	db.Create(&TaskState{ID: "gone", Status: "PAUSED", SandboxPath: "/no/such/dir-xyz"})
	db.Create(&TaskState{ID: "done", Status: "SUCCESS", SandboxPath: liveDir})

	var launched []string
	s := &Server{db: db}
	s.launchFn = func(taskID, sandboxPath, task, file, testCmd string) {
		launched = append(launched, taskID)
	}

	s.RecoverTasks()

	// live → relaunched
	if len(launched) != 1 || launched[0] != "live" {
		t.Fatalf("expected only 'live' relaunched, got %v", launched)
	}
	// gone → marked FAILED
	var gone TaskState
	db.First(&gone, "id = ?", "gone")
	if gone.Status != "FAILED" {
		t.Errorf("gone status: got %q want FAILED", gone.Status)
	}
	// done → untouched, not relaunched
	var done TaskState
	db.First(&done, "id = ?", "done")
	if done.Status != "SUCCESS" {
		t.Errorf("done status changed: %q", done.Status)
	}
}

// ensure the live dir is used (referenced so linters don't flag)
var _ = os.Stat
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `CGO_ENABLED=0 go test ./pkg/orchestrator/ -run 'TestRecoverAction|TestRecoverTasks' -v`
Expected: FAIL to compile — `undefined: recoverAction` / `RecoverTasks`.

- [ ] **Step 3: Write minimal implementation**

```go
// pkg/orchestrator/recovery.go
package orchestrator

import (
	"os"

	"github.com/ibreakthecloud/kiwi/pkg/tunnel"
)

// recoverAction decides how to handle an interrupted task on boot: "relaunch" if
// its sandbox still exists on disk, otherwise "fail".
func recoverAction(sandboxPath string) string {
	if sandboxPath == "" {
		return "fail"
	}
	if _, err := os.Stat(sandboxPath); err != nil {
		return "fail"
	}
	return "relaunch"
}

// RecoverTasks scans for tasks left RUNNING/PAUSED by a previous daemon
// lifetime. Tasks whose sandbox survived are re-launched; the rest are failed.
func (s *Server) RecoverTasks() {
	var tasks []TaskState
	if err := s.db.Where("status IN ?", []string{"RUNNING", "PAUSED"}).Find(&tasks).Error; err != nil {
		return
	}
	for _, t := range tasks {
		if recoverAction(t.SandboxPath) == "relaunch" {
			tunnel.GlobalRegistry.Register(t.ID)
			s.db.Model(&TaskState{}).Where("id = ?", t.ID).
				Update("logs", t.Logs+"\n[Recovery] Re-launched after daemon restart.\n")
			s.launchFn(t.ID, t.SandboxPath, t.Task, t.FilePath, t.TestCmd)
		} else {
			s.db.Model(&TaskState{}).Where("id = ?", t.ID).Updates(map[string]interface{}{
				"status": "FAILED",
				"logs":   t.Logs + "\n[Recovery] Interrupted by daemon restart; sandbox unavailable.\n",
			})
		}
	}
}
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `CGO_ENABLED=0 go test ./pkg/orchestrator/ -run 'TestRecoverAction|TestRecoverTasks' -v`
Expected: PASS.

- [ ] **Step 5: Wire recovery into the daemon**

In `cmd/kiwid/main.go`, after `server := orchestrator.NewServer(db)` and before `server.Start(*addr)`, add:

```go
	server := orchestrator.NewServer(db)

	// Recover tasks interrupted by a previous restart before accepting new work.
	server.RecoverTasks()

	err = server.Start(*addr)
```

- [ ] **Step 6: Build the daemon**

Run: `CGO_ENABLED=0 go build ./cmd/kiwid/`
Expected: no output (success).

- [ ] **Step 7: Commit**

```bash
git add pkg/orchestrator/recovery.go pkg/orchestrator/recovery_test.go cmd/kiwid/main.go
git commit -m "feat(orchestrator): boot recovery for interrupted tasks"
```

---

### Task 4: Idempotency dedupe in `handleTasks`

**Files:**
- Modify: `pkg/orchestrator/server.go` (`handleTasks` POST path)
- Test: `pkg/orchestrator/server_idempotency_test.go`

**Interfaces:**
- Consumes: `findByIdempotencyKey` (Task 1), `Server.launchFn` (Task 2), `TaskState`.
- Produces: idempotent `POST /tasks` behavior.

- [ ] **Step 1: Write the failing integration test**

```go
// pkg/orchestrator/server_idempotency_test.go
package orchestrator

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
)

func multipartTask(t *testing.T) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("task", "fix")
	_ = mw.WriteField("file", "a.go")
	_ = mw.WriteField("test_cmd", "go test ./...")
	fw, _ := mw.CreateFormFile("codebase", "c.zip")
	// minimal valid zip
	var zbuf bytes.Buffer
	zw := zip.NewWriter(&zbuf)
	w, _ := zw.Create("a.go")
	_, _ = w.Write([]byte("package a\n"))
	_ = zw.Close()
	_, _ = fw.Write(zbuf.Bytes())
	_ = mw.Close()
	return &buf, mw.FormDataContentType()
}

func postTask(t *testing.T, s *Server, key string) map[string]string {
	t.Helper()
	body, ctype := multipartTask(t)
	req := httptest.NewRequest(http.MethodPost, "/tasks", body)
	req.Header.Set("Content-Type", ctype)
	req.Header.Set("Authorization", "Bearer "+serverToken())
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	rw := httptest.NewRecorder()
	s.handleTasks(rw, req)
	if rw.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rw.Code, rw.Body.String())
	}
	var out map[string]string
	_ = json.Unmarshal(rw.Body.Bytes(), &out)
	return out
}

func TestIdempotentSubmit(t *testing.T) {
	t.Setenv("KIWI_SERVER_TOKEN", "test-token")
	db := newTestDB(t)
	s := &Server{db: db}
	s.launchFn = func(taskID, sandboxPath, task, file, testCmd string) {} // no engine

	first := postTask(t, s, "dup-key")
	second := postTask(t, s, "dup-key")
	if first["task_id"] != second["task_id"] {
		t.Errorf("same key must dedupe: %s vs %s", first["task_id"], second["task_id"])
	}

	other := postTask(t, s, "different-key")
	if other["task_id"] == first["task_id"] {
		t.Errorf("different key must create a new task")
	}

	var count int64
	db.Model(&TaskState{}).Count(&count)
	if count != 2 {
		t.Errorf("expected 2 rows (dup-key once + different-key), got %d", count)
	}
}
```

> **Note:** this test references `serverToken()` — a small helper the auth middleware already uses to read `KIWI_SERVER_TOKEN`. If the middleware reads the env var inline instead, replace `serverToken()` in the test with `os.Getenv("KIWI_SERVER_TOKEN")` and import `os`. Verify by reading `validateAuth` in `server.go` before running.

- [ ] **Step 2: Run test to verify it fails**

Run: `CGO_ENABLED=0 go test ./pkg/orchestrator/ -run TestIdempotentSubmit -v`
Expected: FAIL — the second POST creates a new task (IDs differ / count is 3), because dedupe isn't implemented yet.

- [ ] **Step 3: Add the dedupe check to `handleTasks`**

In `handleTasks`, immediately after `testCmd := r.FormValue("test_cmd")` and its required-fields check, add:

```go
	// Idempotent submission: if a task already exists for this key, return it.
	idempotencyKey := r.Header.Get("Idempotency-Key")
	if existing, ok := findByIdempotencyKey(s.db, idempotencyKey); ok {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{
			"task_id": existing.ID,
			"status":  existing.Status,
		})
		return
	}
```

Then set the key on the created row — change the `state := &TaskState{...}` literal to include:

```go
		Cost:           0.05,
		IdempotencyKey: idempotencyKey,
```

- [ ] **Step 4: Run test to verify it passes**

Run: `CGO_ENABLED=0 go test ./pkg/orchestrator/ -run TestIdempotentSubmit -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add pkg/orchestrator/server.go pkg/orchestrator/server_idempotency_test.go
git commit -m "feat(orchestrator): idempotent POST /tasks via Idempotency-Key header"
```

---

### Task 5: Client idempotency support

**Files:**
- Modify: `pkg/client/client.go` (`Client` field + `SubmitTask` header)
- Test: `pkg/client/client_idempotency_test.go`
- Modify: `cmd/kiwi/main.go` (`-idempotency-key` flag)

**Interfaces:**
- Consumes: existing `Client`, `New`, `SubmitTask`.
- Produces: `Client.IdempotencyKey` field; `-idempotency-key` CLI flag.

- [ ] **Step 1: Write the failing test**

```go
// pkg/client/client_idempotency_test.go
package client

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSubmitTaskSendsIdempotencyKey(t *testing.T) {
	var gotKey string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotKey = r.Header.Get("Idempotency-Key")
		_, _ = w.Write([]byte(`{"task_id":"x","status":"RUNNING"}`))
	}))
	defer srv.Close()

	c := New(srv.URL, "tok")
	c.IdempotencyKey = "my-key"
	if _, err := c.SubmitTask(context.Background(), "t", "f", "cmd", []byte("z")); err != nil {
		t.Fatal(err)
	}
	if gotKey != "my-key" {
		t.Errorf("header: got %q want my-key", gotKey)
	}
}

func TestSubmitTaskOmitsEmptyIdempotencyKey(t *testing.T) {
	var present bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, present = r.Header["Idempotency-Key"]
		_, _ = w.Write([]byte(`{"task_id":"x","status":"RUNNING"}`))
	}))
	defer srv.Close()

	c := New(srv.URL, "tok") // no key set
	if _, err := c.SubmitTask(context.Background(), "t", "f", "cmd", []byte("z")); err != nil {
		t.Fatal(err)
	}
	if present {
		t.Errorf("Idempotency-Key header should be absent when unset")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `CGO_ENABLED=0 go test ./pkg/client/ -run TestSubmitTask.*Idempotency -v`
Expected: FAIL to compile — `c.IdempotencyKey undefined`.

- [ ] **Step 3: Add the field and header**

In `pkg/client/client.go`, add the field to `Client`:

```go
type Client struct {
	ServerURL      string
	Token          string
	IdempotencyKey string
	HTTP           *http.Client
}
```

In `SubmitTask`, right after setting the Authorization header, add:

```go
	req.Header.Set("Authorization", "Bearer "+c.Token)
	if c.IdempotencyKey != "" {
		req.Header.Set("Idempotency-Key", c.IdempotencyKey)
	}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `CGO_ENABLED=0 go test ./pkg/client/ -run TestSubmitTask.*Idempotency -v`
Expected: PASS (both cases).

- [ ] **Step 5: Add the CLI flag**

In `cmd/kiwi/main.go`, add the flag alongside the others:

```go
	idempotencyKey := flag.String("idempotency-key", "", "optional Idempotency-Key to dedupe retried submissions")
```

Pass it into `run` and set it on the client. Update the `run` signature and call, and after `c := client.New(server, token)` add `c.IdempotencyKey = idempotencyKey`. Concretely:

- Change `run(...)` signature to add `idempotencyKey string` (place it after `token`):

```go
func run(server, token, idempotencyKey, task, file, testCmd, dir, secretsPath string, resume bool, taskID string, interval time.Duration) error {
	c := client.New(server, token)
	c.IdempotencyKey = idempotencyKey
```

- Update the call in `main`:

```go
	if err := run(*server, *token, *idempotencyKey, *task, *file, *testCmd, *dir, *secretsPath, *resume, *taskID, *interval); err != nil {
```

- [ ] **Step 6: Build the client**

Run: `CGO_ENABLED=0 go build ./cmd/kiwi/`
Expected: no output (success).

- [ ] **Step 7: Commit**

```bash
git add pkg/client/client.go pkg/client/client_idempotency_test.go cmd/kiwi/main.go
git commit -m "feat(client): send optional Idempotency-Key header (-idempotency-key flag)"
```

---

### Task 6: Build, restart e2e, and docs

**Files:**
- Modify: `README.md`, `CLAUDE.md`

**Interfaces:** none.

- [ ] **Step 1: Full suite + build both binaries**

Run:
```bash
CGO_ENABLED=0 go test ./pkg/... 2>&1 | tail -8
go build -ldflags="-linkmode=external" -o kiwi ./cmd/kiwi/ && codesign -s - -f ./kiwi
go build -ldflags="-linkmode=external" -o kiwid ./cmd/kiwid/ && codesign -s - -f ./kiwid
```
Expected: all tests PASS; both binaries built and signed.

- [ ] **Step 2: Manual idempotency check (mock mode)**

Run the daemon on a spare port, then submit the same task twice with one key:
```bash
export KIWI_SERVER_TOKEN="rec-token"
./kiwid -addr :8094 -db /tmp/kiwi_rec.db >/tmp/kiwid_rec.log 2>&1 &
sleep 2
./kiwi -server http://localhost:8094 -token rec-token -idempotency-key demo-1 \
  -task "Fix" -file demo_project/math_utils.go -test-cmd "go test ./demo_project/..." | grep "Submitted task"
./kiwi -server http://localhost:8094 -token rec-token -idempotency-key demo-1 \
  -task "Fix" -file demo_project/math_utils.go -test-cmd "go test ./demo_project/..." | grep -E "Submitted task|Task"
```
Expected: the second run reports the **same task id** (deduped). Then stop the daemon: `pkill kiwid; rm -f /tmp/kiwi_rec.db /tmp/kiwid_rec.log kiwi-fix-*.zip`.

- [ ] **Step 3: Update README.md**

Add a short "Durability & Idempotency" subsection under Core Features (or near Getting Started) documenting: tasks persist in SQLite; on restart the daemon re-launches interrupted tasks whose sandbox survived and fails the rest; `POST /tasks` accepts an `Idempotency-Key` header (client `-idempotency-key`) to dedupe retries.

- [ ] **Step 4: Update CLAUDE.md**

Add a `Phase 9 (Completed): Restart Recovery & Idempotency` entry describing `RecoverTasks` boot sweep, the extracted `launchTask`, and header-based dedupe. Add `pkg/orchestrator/recovery.go` and `idempotency.go` to the codebase tree. Update Limitation #4 (in-memory tunnel cache) to note that task execution now recovers on restart when the sandbox survives (the cache is re-populated on client reconnect).

- [ ] **Step 5: Commit**

```bash
git add README.md CLAUDE.md
git commit -m "docs: document restart recovery and idempotency"
```

---

## Self-Review

**Spec coverage:**
- `launchTask` extraction → Task 2. ✅
- Injectable `launchFn` for tests → Task 2. ✅
- `RecoverTasks` (relaunch-if-sandbox-exists, else FAIL) + `recoverAction` → Task 3. ✅
- Daemon wiring (`RecoverTasks()` before `Start`) → Task 3. ✅
- `IdempotencyKey` column + `findByIdempotencyKey` → Task 1. ✅
- Header dedupe in `handleTasks` → Task 4. ✅
- Client field + `-idempotency-key` flag → Task 5. ✅
- Log seeding so recovery preserves prior logs → Task 2 (`launchTask` reads existing row). ✅
- Tests: recoverAction, RecoverTasks (recorder), findByIdempotencyKey, idempotent submit (httptest), client header → Tasks 1,3,4,5. ✅
- Build + restart/idempotency manual + docs → Task 6. ✅

**Placeholder scan:** No TBD/TODO. The one advisory note (Task 4 `serverToken()` vs inline `os.Getenv`) is a verify-before-run instruction with both concrete alternatives given, not a placeholder.

**Type consistency:** `launchFn(taskID, sandboxPath, task, file, testCmd string)` is identical in the `Server` field (Task 2), the `handleTasks` call (Task 2), the `RecoverTasks` call and test recorder (Task 3), and the idempotency test stub (Task 4). `findByIdempotencyKey(db, key) (*TaskState, bool)` matches between Task 1 and Task 4. `TaskState.IdempotencyKey` (Task 1) is set in Task 4. `Client.IdempotencyKey` (Task 5) matches its tests.
