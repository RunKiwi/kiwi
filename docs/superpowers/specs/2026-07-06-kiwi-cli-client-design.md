# Design: `kiwi` CLI Client

**Date:** 2026-07-06
**Status:** Approved (pending spec review)
**Author:** Kiwi cofounders (pairing session)

## Goal

Build the developer-machine half of Kiwi: a `kiwi` CLI that packages a local
codebase, submits a task to the `kiwid` daemon, serves the reverse credential
tunnel so secrets never persist in the cloud, streams live logs, and downloads
the fixed codebase on success. This is the missing piece that makes the whole
system runnable end-to-end (currently only `cmd/kiwid` exists).

## Scope

**In scope**
- `cmd/kiwi/main.go` entrypoint + a testable `pkg/client/` package.
- Submit a new task (zip cwd, multipart POST `/tasks`).
- Serve the reverse tunnel for on-demand secrets (`ANTHROPIC_API_KEY`,
  `GITHUB_TOKEN`, any key the daemon requests) from `secrets.json` → env var.
- Poll task status and stream incremental logs until terminal state.
- On success, download `output.zip` to a local file — **never overwrite local files.**
- Resume an existing task (`-resume -task-id`).

**Out of scope (deliberately)**
- Task cancellation from the client.
- Config files beyond `secrets.json`.
- Retry logic beyond the tunnel's built-in reconnect/backoff.
- Applying the fix in place (safe download only; `--apply` may come later).

## Reused daemon code (no reinvention)

- `sandbox.ZipDir(dir) ([]byte, error)` — packs the codebase, already skips
  `.git`, the `kiwi`/`kiwid` binaries, `secrets.json`, `*.db`, temp/build files.
- `tunnel.ConnectAndListen(ctx, serverURL, taskID, authToken, getSecret func(string) string) error`
  — the reconnecting reverse-tunnel client loop already exists.

## Wire protocol (from the daemon, verbatim)

- **POST `/tasks`** — multipart form fields `task`, `file`, `test_cmd`, and file
  field `codebase` (zip bytes). Response JSON: `{"task_id": "...", "status": "RUNNING"}`.
- **GET `/tasks/{id}`** — JSON of the task: fields include `id`, `status`
  (`RUNNING`/`PAUSED`/`SUCCESS`/`FAILED`), `logs`, `cost`.
- **GET `/tasks/{id}?download=true`** — returns `output.zip` bytes when status is
  `SUCCESS`.
- **Tunnel** — `GET /tunnel/{id}` (chunked stream of requested keys, newline
  delimited) + **POST `/tunnel/{id}/response`** (plain-text secret value). Driven
  entirely by `tunnel.ConnectAndListen`.
- All endpoints require header `Authorization: Bearer <token>`.

## Architecture

### `pkg/client/secrets.go`
```go
// SecretLookup returns a getSecret hook: it reads secretsPath (a JSON object of
// {"KEY":"value"}) once, then resolves each requested key from that map first,
// falling back to os.Getenv(key). Returns "" if neither has it.
func SecretLookup(secretsPath string) func(key string) string
```
- Missing/unreadable/!valid-JSON `secrets.json` → treated as empty map (env-only),
  not a fatal error (the file is optional).

### `pkg/client/logs.go`
```go
// logDelta returns the portion of curr not already present as a prefix in prev.
// If curr does not start with prev (log rewritten), returns curr in full.
func logDelta(prev, curr string) string
```

### `pkg/client/client.go`
```go
type Client struct {
    ServerURL string
    Token     string
    HTTP      *http.Client
}

type TaskStatus struct {
    ID     string  `json:"id"`
    Status string  `json:"status"`
    Logs   string  `json:"logs"`
    Cost   float64 `json:"cost"`
}

func New(serverURL, token string) *Client
func (c *Client) SubmitTask(ctx context.Context, task, file, testCmd string, codebase []byte) (string, error) // returns task_id
func (c *Client) GetStatus(ctx context.Context, taskID string) (TaskStatus, error)
func (c *Client) DownloadResult(ctx context.Context, taskID string) ([]byte, error)
```
- Every request sets the Bearer header. Non-2xx responses return an error that
  includes the status and a hint for 401 ("check -token").
- `ServerURL` is trimmed of a trailing slash.

### `cmd/kiwi/main.go`
Flags:
```
-server    string  default "http://localhost:8080"
-token     string  default os.Getenv("KIWI_SERVER_TOKEN")
-task      string  goal description
-file      string  target file, relative to -dir
-test-cmd  string  test command
-dir       string  default "." (directory to zip)
-secrets   string  default "secrets.json"
-resume    bool    default false
-task-id   string  (required with -resume)
-interval  duration default 2s
```

Flow (new task):
1. Validate flags (`-task`, `-file`, `-test-cmd` required unless `-resume`).
2. `zip, err := sandbox.ZipDir(dir)`.
3. `taskID, err := client.SubmitTask(ctx, task, file, testCmd, zip)`; print it.
4. `go tunnel.ConnectAndListen(ctx, server, taskID, token, client.SecretLookup(secretsPath))`.
5. Poll loop every `interval`: `GetStatus`, print `logDelta(prev, cur.Logs)`, update
   `prev`; stop when status is `SUCCESS` or `FAILED`.
6. On `SUCCESS`: `DownloadResult` → write `./kiwi-fix-<taskID>.zip`, print the path.
7. `cancel()` the context so the tunnel goroutine exits; exit non-zero on `FAILED`.

Flow (resume): skip steps 2–3; use `-task-id`; run steps 4–7.

## Error handling

- Missing required flags → usage message, exit 2.
- `ZipDir` / network / non-2xx → printed error, exit 1.
- Missing secret → tunnel hook returns `""`; the daemon re-pauses and the client
  keeps polling (status shows `PAUSED`); print a one-line hint naming the key.
- Tunnel connection drops → `ConnectAndListen` reconnects automatically.
- `FAILED` terminal status → print final logs, exit 1.

## Testing

- `SecretLookup`: secrets.json hit; env fallback; secrets.json precedence over env;
  missing file → env-only; malformed JSON → env-only. Table tests, no network.
- `logDelta`: empty prev; growing logs (prefix); identical (no delta); rewritten
  logs (non-prefix → full).
- `Client.SubmitTask` / `GetStatus` / `DownloadResult`: against `httptest.Server`
  asserting method, path, Bearer header, multipart fields, and response decoding.
  No real network.

## Build / verification

- Build both binaries per CLAUDE.md (external linkmode + ad-hoc codesign), using
  the `./cmd/kiwi/` path form (Go ≥ 1.24 requires the `./` prefix).
- `CGO_ENABLED=0 go test ./pkg/...` green.
- Manual e2e: start `kiwid` (mock mode), run `kiwi` against the `demo_project`
  task, confirm SUCCESS + a downloaded `kiwi-fix-<id>.zip`. Optionally repeat with
  `KIWI_LLM_PROVIDER=anthropic` and a key in `secrets.json` to exercise the tunnel.

## Risks / notes

- The daemon's tunnel `GetSecret` has a 5s send / 10s receive timeout; the client
  must keep `ConnectAndListen` running for the whole task, which the design does.
- `sandbox.ZipDir` skips `secrets.json`, so credentials are never uploaded even
  though the client reads them locally — good, and worth an explicit test note.
