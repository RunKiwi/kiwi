# kiwi CLI Client Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build the `kiwi` CLI client that packages a local codebase, submits a task to the `kiwid` daemon, serves the reverse credential tunnel, streams logs, and downloads the fixed codebase on success.

**Architecture:** A testable `pkg/client/` package (HTTP client, secret lookup, log delta) plus a thin `cmd/kiwi/main.go` that wires them together with the existing `sandbox.ZipDir` (codebase packing) and `tunnel.ConnectAndListen` (reverse tunnel). Non-destructive: the fix is downloaded to a zip, never applied over local files.

**Tech Stack:** Go 1.24, stdlib `net/http`/`mime/multipart`/`flag`, existing `pkg/sandbox` and `pkg/tunnel` packages, `net/http/httptest` for tests.

## Global Constraints

- Module path: `github.com/ibreakthecloud/kiwi`.
- Build/sign per CLAUDE.md, using the `./cmd/kiwi/` path form (Go ≥ 1.24 needs the `./` prefix).
- All daemon requests send header `Authorization: Bearer <token>`.
- Non-destructive: on success write `./kiwi-fix-<taskID>.zip`; never overwrite local files.
- Secret resolution order: `secrets.json` map first, then `os.Getenv(key)`; missing/malformed file is non-fatal (env-only).
- Tests run with `CGO_ENABLED=0 go test ./pkg/...`; tests never hit a real network (use `httptest`).
- Reuse `sandbox.ZipDir` and `tunnel.ConnectAndListen` — do not reimplement zipping or the tunnel.

---

### Task 1: Secret lookup (`SecretLookup`)

**Files:**
- Create: `pkg/client/secrets.go`
- Test: `pkg/client/secrets_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `func SecretLookup(secretsPath string) func(key string) string`.

- [ ] **Step 1: Write the failing test**

```go
// pkg/client/secrets_test.go
package client

import (
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestSecretLookup(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "secrets.json", `{"GITHUB_TOKEN":"from-file"}`)

	t.Setenv("ANTHROPIC_API_KEY", "from-env")
	t.Setenv("GITHUB_TOKEN", "env-should-lose")

	get := SecretLookup(path)

	if v := get("GITHUB_TOKEN"); v != "from-file" {
		t.Errorf("file precedence: got %q want from-file", v)
	}
	if v := get("ANTHROPIC_API_KEY"); v != "from-env" {
		t.Errorf("env fallback: got %q want from-env", v)
	}
	if v := get("MISSING"); v != "" {
		t.Errorf("missing key: got %q want empty", v)
	}
}

func TestSecretLookupNoFile(t *testing.T) {
	t.Setenv("ONLY_ENV", "yes")
	get := SecretLookup(filepath.Join(t.TempDir(), "does-not-exist.json"))
	if v := get("ONLY_ENV"); v != "yes" {
		t.Errorf("env-only: got %q want yes", v)
	}
}

func TestSecretLookupMalformed(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "bad.json", `{not valid json`)
	t.Setenv("K", "v")
	get := SecretLookup(path)
	if v := get("K"); v != "v" {
		t.Errorf("malformed → env-only: got %q want v", v)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `CGO_ENABLED=0 go test ./pkg/client/ -run TestSecretLookup -v`
Expected: FAIL to compile — `undefined: SecretLookup`.

- [ ] **Step 3: Write minimal implementation**

```go
// pkg/client/secrets.go
package client

import (
	"encoding/json"
	"os"
)

// SecretLookup returns a getSecret hook for the reverse tunnel. It reads
// secretsPath once (a JSON object of {"KEY":"value"}); each requested key is
// resolved from that map first, then from the process environment. A missing or
// malformed file is treated as an empty map (environment-only), not an error.
func SecretLookup(secretsPath string) func(key string) string {
	fileSecrets := map[string]string{}
	if data, err := os.ReadFile(secretsPath); err == nil {
		_ = json.Unmarshal(data, &fileSecrets) // malformed → empty map, env-only
	}
	return func(key string) string {
		if v, ok := fileSecrets[key]; ok {
			return v
		}
		return os.Getenv(key)
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `CGO_ENABLED=0 go test ./pkg/client/ -run TestSecretLookup -v`
Expected: PASS (all three cases).

- [ ] **Step 5: Commit**

```bash
git add pkg/client/secrets.go pkg/client/secrets_test.go
git commit -m "feat(client): add secret lookup (secrets.json then env)"
```

---

### Task 2: Log delta helper (`logDelta`)

**Files:**
- Create: `pkg/client/logs.go`
- Test: `pkg/client/logs_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `func logDelta(prev, curr string) string`.

- [ ] **Step 1: Write the failing test**

```go
// pkg/client/logs_test.go
package client

import "testing"

func TestLogDelta(t *testing.T) {
	if got := logDelta("", "hello"); got != "hello" {
		t.Errorf("empty prev: got %q", got)
	}
	if got := logDelta("hello", "hello world"); got != " world" {
		t.Errorf("growing: got %q", got)
	}
	if got := logDelta("hello", "hello"); got != "" {
		t.Errorf("no change: got %q", got)
	}
	if got := logDelta("abc", "xyz123"); got != "xyz123" {
		t.Errorf("rewritten (non-prefix): got %q", got)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `CGO_ENABLED=0 go test ./pkg/client/ -run TestLogDelta -v`
Expected: FAIL to compile — `undefined: logDelta`.

- [ ] **Step 3: Write minimal implementation**

```go
// pkg/client/logs.go
package client

import "strings"

// logDelta returns the portion of curr that follows prev when curr grows by
// appending. If curr is not an extension of prev (logs were rewritten), it
// returns curr in full.
func logDelta(prev, curr string) string {
	if strings.HasPrefix(curr, prev) {
		return curr[len(prev):]
	}
	return curr
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `CGO_ENABLED=0 go test ./pkg/client/ -run TestLogDelta -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add pkg/client/logs.go pkg/client/logs_test.go
git commit -m "feat(client): add incremental log delta helper"
```

---

### Task 3: HTTP client (`Client` + SubmitTask/GetStatus/DownloadResult)

**Files:**
- Create: `pkg/client/client.go`
- Test: `pkg/client/client_test.go`

**Interfaces:**
- Consumes: nothing from earlier tasks.
- Produces:
  - `type Client struct { ServerURL string; Token string; HTTP *http.Client }`
  - `type TaskStatus struct { ID string; Status string; Logs string; Cost float64 }` (JSON tags `id`,`status`,`logs`,`cost`)
  - `func New(serverURL, token string) *Client`
  - `func (c *Client) SubmitTask(ctx context.Context, task, file, testCmd string, codebase []byte) (string, error)`
  - `func (c *Client) GetStatus(ctx context.Context, taskID string) (TaskStatus, error)`
  - `func (c *Client) DownloadResult(ctx context.Context, taskID string) ([]byte, error)`

- [ ] **Step 1: Write the failing test**

```go
// pkg/client/client_test.go
package client

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSubmitTask(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/tasks" || r.Method != http.MethodPost {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer tok" {
			t.Errorf("auth header: got %q", got)
		}
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Fatalf("parse multipart: %v", err)
		}
		if r.FormValue("task") != "fix it" || r.FormValue("file") != "a.go" || r.FormValue("test_cmd") != "go test ./..." {
			t.Errorf("form values wrong: %v", r.MultipartForm.Value)
		}
		f, _, err := r.FormFile("codebase")
		if err != nil {
			t.Fatalf("codebase file: %v", err)
		}
		b, _ := io.ReadAll(f)
		if string(b) != "ZIPBYTES" {
			t.Errorf("codebase content: got %q", string(b))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"task_id":"abc123","status":"RUNNING"}`))
	}))
	defer srv.Close()

	c := New(srv.URL, "tok")
	id, err := c.SubmitTask(context.Background(), "fix it", "a.go", "go test ./...", []byte("ZIPBYTES"))
	if err != nil {
		t.Fatalf("SubmitTask: %v", err)
	}
	if id != "abc123" {
		t.Errorf("task id: got %q want abc123", id)
	}
}

func TestGetStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/tasks/abc123" {
			t.Errorf("path: got %q", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"id":"abc123","status":"SUCCESS","logs":"done","cost":0.42}`))
	}))
	defer srv.Close()

	c := New(srv.URL, "tok")
	st, err := c.GetStatus(context.Background(), "abc123")
	if err != nil {
		t.Fatalf("GetStatus: %v", err)
	}
	if st.Status != "SUCCESS" || st.Logs != "done" || st.Cost != 0.42 {
		t.Errorf("status decode wrong: %+v", st)
	}
}

func TestGetStatusAuthError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	defer srv.Close()

	c := New(srv.URL, "bad")
	_, err := c.GetStatus(context.Background(), "abc123")
	if err == nil || !strings.Contains(err.Error(), "401") {
		t.Errorf("expected 401 error, got %v", err)
	}
}

func TestDownloadResult(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("download") != "true" {
			t.Errorf("missing download=true: %q", r.URL.RawQuery)
		}
		_, _ = w.Write([]byte("ZIPRESULT"))
	}))
	defer srv.Close()

	c := New(srv.URL, "tok")
	b, err := c.DownloadResult(context.Background(), "abc123")
	if err != nil {
		t.Fatalf("DownloadResult: %v", err)
	}
	if string(b) != "ZIPRESULT" {
		t.Errorf("result: got %q", string(b))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `CGO_ENABLED=0 go test ./pkg/client/ -run 'TestSubmitTask|TestGetStatus|TestDownloadResult' -v`
Expected: FAIL to compile — `undefined: New` etc.

- [ ] **Step 3: Write minimal implementation**

```go
// pkg/client/client.go
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
)

// Client talks to the kiwid daemon HTTP API.
type Client struct {
	ServerURL string
	Token     string
	HTTP      *http.Client
}

// TaskStatus is the subset of the daemon's task row the client consumes.
type TaskStatus struct {
	ID     string  `json:"id"`
	Status string  `json:"status"`
	Logs   string  `json:"logs"`
	Cost   float64 `json:"cost"`
}

// New builds a Client with a default HTTP client.
func New(serverURL, token string) *Client {
	return &Client{
		ServerURL: strings.TrimSuffix(serverURL, "/"),
		Token:     token,
		HTTP:      http.DefaultClient,
	}
}

func (c *Client) authErr(resp *http.Response) error {
	body, _ := io.ReadAll(resp.Body)
	msg := strings.TrimSpace(string(body))
	if resp.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("daemon returned 401 Unauthorized (check -token): %s", msg)
	}
	return fmt.Errorf("daemon returned %d: %s", resp.StatusCode, msg)
}

// SubmitTask uploads the zipped codebase and task parameters, returning the task ID.
func (c *Client) SubmitTask(ctx context.Context, task, file, testCmd string, codebase []byte) (string, error) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for k, v := range map[string]string{"task": task, "file": file, "test_cmd": testCmd} {
		if err := mw.WriteField(k, v); err != nil {
			return "", err
		}
	}
	fw, err := mw.CreateFormFile("codebase", "codebase.zip")
	if err != nil {
		return "", err
	}
	if _, err := fw.Write(codebase); err != nil {
		return "", err
	}
	if err := mw.Close(); err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.ServerURL+"/tasks", &buf)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+c.Token)

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", c.authErr(resp)
	}
	var out struct {
		TaskID string `json:"task_id"`
		Status string `json:"status"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	return out.TaskID, nil
}

// GetStatus fetches the current task row.
func (c *Client) GetStatus(ctx context.Context, taskID string) (TaskStatus, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.ServerURL+"/tasks/"+taskID, nil)
	if err != nil {
		return TaskStatus{}, err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return TaskStatus{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return TaskStatus{}, c.authErr(resp)
	}
	var st TaskStatus
	if err := json.NewDecoder(resp.Body).Decode(&st); err != nil {
		return TaskStatus{}, err
	}
	return st, nil
}

// DownloadResult fetches the fixed codebase zip for a successful task.
func (c *Client) DownloadResult(ctx context.Context, taskID string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.ServerURL+"/tasks/"+taskID+"?download=true", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, c.authErr(resp)
	}
	return io.ReadAll(resp.Body)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `CGO_ENABLED=0 go test ./pkg/client/ -v`
Expected: PASS (all client + secrets + logs tests).

- [ ] **Step 5: Commit**

```bash
git add pkg/client/client.go pkg/client/client_test.go
git commit -m "feat(client): add HTTP client for submit/status/download"
```

---

### Task 4: CLI entrypoint (`cmd/kiwi/main.go`)

**Files:**
- Create: `cmd/kiwi/main.go`

**Interfaces:**
- Consumes: `client.New`, `client.SubmitTask`, `client.GetStatus`, `client.DownloadResult`, `client.SecretLookup` (Tasks 1 & 3); `sandbox.ZipDir`; `tunnel.ConnectAndListen`.
- Produces: the `kiwi` binary (no exported Go API).

- [ ] **Step 1: Write the entrypoint**

```go
// cmd/kiwi/main.go
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/ibreakthecloud/kiwi/pkg/client"
	"github.com/ibreakthecloud/kiwi/pkg/sandbox"
	"github.com/ibreakthecloud/kiwi/pkg/tunnel"
)

func main() {
	server := flag.String("server", "http://localhost:8080", "kiwid daemon URL")
	token := flag.String("token", os.Getenv("KIWI_SERVER_TOKEN"), "Bearer auth token")
	task := flag.String("task", "", "task/goal description")
	file := flag.String("file", "", "target file, relative to -dir")
	testCmd := flag.String("test-cmd", "", "test command the daemon runs")
	dir := flag.String("dir", ".", "directory to zip and upload")
	secretsPath := flag.String("secrets", "secrets.json", "path to secrets.json")
	resume := flag.Bool("resume", false, "resume an existing task instead of submitting")
	taskID := flag.String("task-id", "", "task ID to resume (with -resume)")
	interval := flag.Duration("interval", 2*time.Second, "status poll interval")
	flag.Parse()

	if err := run(*server, *token, *task, *file, *testCmd, *dir, *secretsPath, *resume, *taskID, *interval); err != nil {
		fmt.Fprintf(os.Stderr, "\n[kiwi] error: %v\n", err)
		os.Exit(1)
	}
}

func run(server, token, task, file, testCmd, dir, secretsPath string, resume bool, taskID string, interval time.Duration) error {
	c := client.New(server, token)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if resume {
		if taskID == "" {
			return fmt.Errorf("-resume requires -task-id")
		}
		fmt.Printf("[kiwi] Resuming task %s\n", taskID)
	} else {
		if task == "" || file == "" || testCmd == "" {
			return fmt.Errorf("-task, -file, and -test-cmd are required")
		}
		fmt.Printf("[kiwi] Packaging %s ...\n", dir)
		zipBytes, err := sandbox.ZipDir(dir)
		if err != nil {
			return fmt.Errorf("failed to package codebase: %w", err)
		}
		id, err := c.SubmitTask(ctx, task, file, testCmd, zipBytes)
		if err != nil {
			return fmt.Errorf("failed to submit task: %w", err)
		}
		taskID = id
		fmt.Printf("[kiwi] Submitted task %s\n", taskID)
	}

	// Serve the reverse credential tunnel in the background.
	go func() {
		_ = tunnel.ConnectAndListen(ctx, server, taskID, token, client.SecretLookup(secretsPath))
	}()

	// Poll status and stream logs until terminal state.
	prevLogs := ""
	for {
		st, err := c.GetStatus(ctx, taskID)
		if err != nil {
			return fmt.Errorf("failed to get status: %w", err)
		}
		if delta := client.LogDelta(prevLogs, st.Logs); delta != "" {
			fmt.Print(delta)
			prevLogs = st.Logs
		}
		switch st.Status {
		case "SUCCESS":
			fmt.Printf("\n[kiwi] Task SUCCESS (cost $%.4f)\n", st.Cost)
			out, err := c.DownloadResult(ctx, taskID)
			if err != nil {
				return fmt.Errorf("failed to download result: %w", err)
			}
			outPath := fmt.Sprintf("kiwi-fix-%s.zip", taskID)
			if err := os.WriteFile(outPath, out, 0644); err != nil {
				return fmt.Errorf("failed to write result: %w", err)
			}
			fmt.Printf("[kiwi] Fixed codebase saved to %s\n", outPath)
			return nil
		case "FAILED":
			return fmt.Errorf("task FAILED (see logs above)")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(interval):
		}
	}
}
```

> **Note:** `logDelta` is unexported in Task 2. The entrypoint needs it exported. In this task, add an exported wrapper to `pkg/client/logs.go` so `main` can call it (see Step 2). Do not rename the tested `logDelta`.

- [ ] **Step 2: Export the log delta for the CLI**

Append to `pkg/client/logs.go`:

```go
// LogDelta is the exported wrapper around logDelta for CLI use.
func LogDelta(prev, curr string) string { return logDelta(prev, curr) }
```

- [ ] **Step 3: Build the client binary**

Run: `CGO_ENABLED=0 go build ./cmd/kiwi/`
Expected: no output (compiles).

- [ ] **Step 4: Run the full test suite**

Run: `CGO_ENABLED=0 go test ./pkg/... -v`
Expected: PASS (client package + existing packages).

- [ ] **Step 5: Commit**

```bash
git add cmd/kiwi/main.go pkg/client/logs.go
git commit -m "feat(cli): add kiwi client entrypoint (submit, tunnel, poll, download)"
```

---

### Task 5: Build, sign, and end-to-end verification (mock mode)

**Files:** none (build + docs only).

**Interfaces:** none.

- [ ] **Step 1: Build and sign both binaries**

Run:
```bash
go build -ldflags="-linkmode=external" -o kiwi ./cmd/kiwi/ && codesign -s - -f ./kiwi
go build -ldflags="-linkmode=external" -o kiwid ./cmd/kiwid/ && codesign -s - -f ./kiwid
```
Expected: both binaries built and signed.

- [ ] **Step 2: Start the daemon (mock mode) in the background**

Run:
```bash
export KIWI_SERVER_TOKEN="demo-token"
./kiwid -addr :8080 -db /tmp/kiwi_e2e.db
```
Expected: `[Server] Kiwi daemon listening on :8080`.

- [ ] **Step 3: Submit the demo task with the client**

Run (in another shell, from the repo root):
```bash
./kiwi -server http://localhost:8080 -token demo-token \
  -task "Fix division by zero in Divide()" \
  -file demo_project/math_utils.go \
  -test-cmd "go test ./demo_project/..."
```
Expected: streamed `[Orchestrator]`/`[Actor]`/`[Critic]`/`[Gate]` logs, then `Task SUCCESS`, then `Fixed codebase saved to kiwi-fix-<id>.zip`. Confirm the zip exists: `ls kiwi-fix-*.zip`.

- [ ] **Step 4: Stop the daemon and clean up**

Run:
```bash
kill %1 2>/dev/null || pkill kiwid
rm -f /tmp/kiwi_e2e.db kiwi-fix-*.zip
```
Expected: daemon stops, artifacts removed.

- [ ] **Step 5: Update CLAUDE.md**

Edit `CLAUDE.md`:
- Under the codebase overview, add `cmd/kiwi/main.go` and `pkg/client/` to the tree (the client now exists).
- Note that the `kiwi` CLI client is implemented (submit, reverse tunnel, log streaming, non-destructive result download; `-resume` supported).

- [ ] **Step 6: Commit**

```bash
git add CLAUDE.md
git commit -m "docs: document the kiwi CLI client"
```

---

## Self-Review

**Spec coverage:**
- `pkg/client/secrets.go` (secrets.json → env) → Task 1. ✅
- `pkg/client/logs.go` (logDelta) → Task 2. ✅
- `pkg/client/client.go` (SubmitTask/GetStatus/DownloadResult, Bearer, 401 hint) → Task 3. ✅
- `cmd/kiwi/main.go` (flags, zip via ZipDir, tunnel via ConnectAndListen, poll, non-destructive download, resume) → Task 4. ✅
- Reuse of `sandbox.ZipDir` + `tunnel.ConnectAndListen` → Task 4. ✅
- Non-destructive `kiwi-fix-<id>.zip` → Task 4. ✅
- Build/sign with `./` path form + mock e2e → Task 5. ✅
- Tests via httptest, no network → Tasks 1–3. ✅

**Placeholder scan:** No TBD/TODO; every code step shows full code; every run step shows command + expected result.

**Type consistency:** `SecretLookup`, `logDelta`/`LogDelta`, `Client`, `New`, `TaskStatus{ID,Status,Logs,Cost}`, `SubmitTask`, `GetStatus`, `DownloadResult` are used identically across Tasks 1–4. The one gotcha — `logDelta` is unexported (tested) and `main` needs `LogDelta` — is handled explicitly in Task 4 Step 2 with an exported wrapper.
