# Kiwi Development Guide (CLAUDE.md)

This file provides system context, build instructions, styling guidelines, and current status for AI assistants and contributors working on the **Kiwi** codebase.

---

## 1. Commands & Workflows

### Compilation & Signing (macOS workaround)
Because of the newer macOS `dyld` dynamic linker requirements (`missing LC_UUID load command`), Go binaries must be compiled with external linking and ad-hoc signed:

```bash
# Build Client CLI
go build -ldflags="-linkmode=external" -o kiwi cmd/kiwi/main.go
codesign -s - -f ./kiwi

# Build Server Daemon
go build -ldflags="-linkmode=external" -o kiwid cmd/kiwid/main.go
codesign -s - -f ./kiwid
```

### Running the Services
```bash
# Start the central cloud daemon with SQLite persistence and Docker sandboxing enabled
export USE_DOCKER="true"
export KIWI_SERVER_TOKEN="my-secret-token-1234"
./kiwid -addr :8080 -db kiwi.db

# Execute a TDD auto-fixing task using Bearer Token authorization
./kiwi -token "my-secret-token-1234" -task "Fix division by zero in Divide()" -file demo_project/math_utils.go -test-cmd "go test ./demo_project/..."

# Resume a paused cloud task (waiting for credentials tunnel reconnect)
./kiwi -token "my-secret-token-1234" -resume -task-id <task_id>
```

### Launching the Dashboard Frontend
The dashboard is separated from the Go binary into a standalone single-page web app in the `/web/` folder. It can be served from any static port or opened directly:

```bash
# Option A: Start a static file server on port 3000
npx -y serve -l 3000 web/

# Option B: Open the index.html page directly in browser
open web/index.html
```

*Note: Configure the Kiwi Daemon URL (e.g. `http://localhost:8080`) and your Bearer Authorization token inside the dashboard Settings gear panel in the upper-right corner (persists in `localStorage`).*

### Testing the Packages
```bash
# Run unit tests (CGO_ENABLED=0 bypasses macOS dyld UUID checks for tests)
CGO_ENABLED=0 go test -v ./pkg/...
```

### Mandatory Pre-Commit Checks (run EVERY time, before every commit/PR)
CI (`.github/workflows/ci.yml`) enforces these on every PR and push to `main`. Run them locally **before every commit** so the build never lands red — this is exactly how stacked semantic-merge breakage (import cycles, removed SDK helpers, renamed fields) has snuck onto `main` before:

```bash
gofmt -l cmd/ pkg/                 # MUST print nothing (formatting). Fix with: gofmt -w cmd/ pkg/
CGO_ENABLED=0 go vet ./...         # MUST be clean (catches import cycles, bad struct fields, etc.)
CGO_ENABLED=0 go test ./pkg/...    # MUST pass
CGO_ENABLED=0 go build ./...       # MUST build all packages and both binaries
```

Treat any non-empty `gofmt` output, `go vet` finding, test failure, or build error as a hard blocker — do not commit until all four are green. When resolving a merge against `main`, run all four on the merged tree before pushing (a clean per-PR CI does not guarantee the merged result compiles).

---

## 2. Codebase Overview

```
kiwi/
├── cmd/
│   ├── kiwi/
│   │   └── main.go          # CLI client (zips codebase, auth headers, polls logs, downloads fixes)
│   └── kiwid/
│       └── main.go          # Cloud Daemon entrypoint (starts HTTP server, DB migrations)
├── pkg/
│   ├── orchestrator/
│   │   ├── engine.go        # Actor-Critic feedback loop orchestrator (caching resolver, safety gates)
│   │   ├── server.go        # HTTP tasks execution controller (/tasks, /tunnel, auth, CORS, launchTask)
│   │   ├── recovery.go      # Boot recovery: re-launch or fail tasks interrupted by a restart
│   │   ├── idempotency.go   # Idempotency-Key lookup for deduping task submissions
│   │   ├── events.go        # TaskEvent model + per-phase loop telemetry (summarize helper)
│   │   └── db.go            # SQLite GORM persistence helper
│   ├── sandbox/
│   │   ├── exec.go          # Sandbox executor (local execution or isolated Docker container execution)
│   │   └── sync.go          # Workspace zip/unzip package with Zip Slip protection and database filtering
│   ├── provider/
│   │   ├── mock.go          # Offline simulated LLM Actor-Critic rules (default)
│   │   ├── llm.go           # Live Anthropic provider (Actor + Critic, claude-opus-4-8)
│   │   ├── critic.go        # Critic/Verdict/UsageReporter interfaces + MockCritic
│   │   └── parse.go         # Fenced-code extraction, verdict JSON parsing, token pricing
│   ├── client/
│   │   ├── client.go        # HTTP client: SubmitTask / GetStatus / DownloadResult (Bearer auth)
│   │   ├── secrets.go       # SecretLookup: secrets.json then env var, for the tunnel hook
│   │   └── logs.go          # Incremental log-delta helper for live streaming
│   ├── tunnel/
│   │   └── tunnel.go        # Reverse credential tunnel multiplexer with memory caching
│   └── dashboard/
│       └── dashboard.go     # Embedded HTML/JS Kanban board UI (Deprecated fallback)
├── web/                     # Decoupled Standalone Dashboard Frontend
│   ├── index.html           # Main UI DOM layout with settings configurations
│   ├── style.css            # Dark-mode premium CSS styles
│   └── app.js               # Ajax request client with localStorage credentials mapping
├── demo_project/            # A buggy Go project used for verification testing
└── go.mod
```

---

## 3. Current Implementation Status

*   **Phase 1 (Completed)**: Core local Actor-Critic Loop Engine that corrects local compilation/test failures using LLM edit mock rules.
*   **Phase 2 (Completed)**: Cloud Sandbox & Workspace Sync. Zips local codebase, uploads it to the daemon, runs execution loops inside sandbox directories, and syncs fixes back.
*   **Phase 3 (Completed)**: Reverse Credential Tunneling. Relays developer credentials (`GITHUB_TOKEN`) on-demand from local client to cloud sandbox.
*   **Phase 4 (Completed)**: Embedded Kanban Dashboard, Cost Budgets, & Loop Safety.
*   **Phase 5 (Completed)**: Authentication & Docker Sandboxing:
    *   **Bearer Token Authorization**: Enforces API access checks. Uses custom tokens (via `KIWI_SERVER_TOKEN`) to validate CLI requests.
    *   **Docker Container Sandboxing**: Spawns isolated `golang:1.21-alpine` containers to execute compiler and test commands, preventing host pollution.
    *   **Credentials Caching**: Caches resolved credentials in-memory on first fetch. Allows developers to close their laptops and sever the tunnel while the cloud loop continues executing to completion.
*   **Phase 6 (Completed)**: Standalone Frontend & CORS Support:
    *   **Decoupled Frontend**: Moved the dashboard UI out of the Go daemon into a dedicated `/web/` directory containing static HTML, CSS, and JS.
    *   **CORS Preflight Middleware**: Added CORS handling to `kiwid` daemon task endpoints to permit cross-origin requests from the standalone browser page.
    *   **Token Authorization Settings Panel**: Added a configuration gear widget in the UI to save daemon URLs and Bearer Tokens inside browser `localStorage`.
*   **Phase 7 (Completed)**: Live Anthropic LLM Actor–Critic:
    *   **Live Claude Provider**: `pkg/provider/llm.go` adds `AnthropicProvider` (model `claude-opus-4-8`, adaptive thinking) implementing both the Actor (`GetCodeEdit`) and a new `Critic` (`ReviewEdit`) interface. Selected via `KIWI_LLM_PROVIDER=anthropic`; the rule-based mock remains the default for offline/test runs.
    *   **Real Critic Gate**: The engine now runs Actor → Critic → (apply on approval) → tests. The Critic reviews each diff before it is applied and can reject it, feeding its reasons back to the Actor; tests remain the final gate. Actor↔Critic retries are bounded by the existing budget/step/duplicate-output limits.
    *   **Tunnel-Resolved API Key**: `ANTHROPIC_API_KEY` is resolved through the reverse credential tunnel (in-memory cached), falling back to the daemon env var, and pauses statefully if neither is available. The key is never persisted or logged.
    *   **Token-Based Cost & Config**: Cost is computed from real token usage (Opus 4.8 pricing) and drives the existing budget gate. Task timeout (`KIWI_TASK_TIMEOUT`, default 10m) and budget (`KIWI_MAX_BUDGET`, default $1.00) are configurable.
*   **Phase 8 (Completed)**: `kiwi` CLI Client (`cmd/kiwi`, `pkg/client`):
    *   **Task Submission**: Packages the working directory via `sandbox.ZipDir` (skips `.git`, binaries, `secrets.json`) and uploads it to `POST /tasks` with Bearer auth.
    *   **Reverse Tunnel Serving**: Drives `tunnel.ConnectAndListen`, answering the daemon's on-demand secret requests via `SecretLookup` (`secrets.json` first, then environment). Secrets never leave the machine except transiently.
    *   **Live Log Streaming**: Polls `GET /tasks/{id}` and prints incremental log deltas until a terminal state.
    *   **Non-Destructive Result Download**: On success, downloads the fixed codebase to `kiwi-fix-<task-id>.zip` — local files are never overwritten. Supports `-resume -task-id` to reconnect to a paused task.
*   **Phase 9 (Completed)**: Pull Request Continuous Integration:
    *   **CI Pipeline**: Configured a GitHub Actions workflow in `.github/workflows/ci.yml` that runs on all pull requests and pushes to `main`.
    *   **Verification**: Automatically sets up Go `1.24` and executes formatting checks (`gofmt`), static analysis (`go vet`), unit tests (`go test`), and builds the binary targets for client and daemon on Linux.
*   **Phase 10 (Completed)**: Restart Recovery & Idempotency (`recovery.go`, `idempotency.go`):
    *   **Injectable `launchTask`**: The task-run goroutine was extracted from `handleTasks` into `Server.launchTask` (behind an injectable `launchFn` for testing), so both submission and recovery share one code path. It seeds its log buffer from the existing row so recovered tasks keep prior logs.
    *   **Boot Recovery (`RecoverTasks`)**: On startup (called from `cmd/kiwid` before `Start`), the daemon scans `RUNNING`/`PAUSED` rows. If the task's sandbox still exists on disk it re-registers the tunnel and re-launches the loop; otherwise it marks the task `FAILED` with an "interrupted by restart" note. Eliminates zombie tasks.
    *   **Idempotent Submission**: `POST /tasks` reads an optional `Idempotency-Key` header; if a task with that key exists it returns the original `{task_id,status}` with no new sandbox/run. Client exposes `-idempotency-key`. (Single-daemon: dedupe is SELECT-then-create; the rare concurrent double-submit race is accepted.)
*   **Phase 11 (Completed)**: Identity, Access Control, and Multi-Tenancy (`pkg/auth/`, `server.go`, `idempotency.go`, `db.go`):
    *   **Organization, User, & API Key Models**: Introduced multi-tenant database schemas with SHA-256 hashed API keys.
    *   **Auth Middleware**: Added `AuthMiddleware` and context claims injection, replacing static server tokens with tenant-scoped API keys (with a bootstrap env fallback).
    *   **Owner Scoping & Filtering**: Scoped all task REST endpoints by tenant. Non-admins only see tasks created in their organization. Idempotency checks are partitioned per org.
*   **Phase 12 (Completed)**: Reverse Tunnel & Secrets Isolation (`pkg/tunnel/`, `server.go`, `recovery.go`):
    *   **Task/Tunnel Org Ownership**: Embedded tenant metadata in reverse tunnels. Connect/response endpoints verify caller identity against task ownership. Tunnels are automatically deregistered on task completion.
*   **Phase 13 (Completed)**: Per-Tenant Resource Limits & Sandbox Isolation (`pkg/auth/limits.go`, `server.go`, `engine.go`, `exec.go`):
    *   **OrgLimits Model & Checks**: Introduced `OrgLimits` database table for tracking max concurrent tasks, budget caps, timeout constraints, sandbox disk size, and tenant-specific Docker images. Enforced concurrency checks and monthly budget aggregates during task submission.
    *   **Configurable Sandbox Constraints**: Refactored the command execution sandbox to support context-based configuration retrieval. Tasks now run with custom CPU, memory limits, and complete network isolation (`--network=none`), preventing malicious external connectivity.
    *   **Disk Quotas & Sandbox Partitioning**: Implemented unzipped directory size validation, rejecting submissions exceeding the configured size limit with 413. Prefixed sandbox paths with tenant IDs to ensure filesystem segregation.
*   **Phase 14 (Completed)**: Per-Tenant Cost Tracking & Billing (`pkg/billing/billing.go`, `server.go`, `admin.go`, `parse.go`):
    *   **Usage Aggregation Service**: Created `pkg/billing/` package to aggregate metrics (total cost, task counts, success/failure ratios, and top spenders list) dynamically over Unix timestamps or RFC3339 date ranges.
    *   **Org & Admin Billing Endpoints**: Added a tenant-scoped `/usage` API and an admin-only `/admin/orgs/{orgID}/usage` API endpoint.
    *   **Model-Based Pricing**: Refactored cost calculation to look up rates from a model pricing map (supporting custom token prices for Claude Opus, Sonnet, and Haiku).
*   **Phase 15 (Completed)**: Per-Tenant LLM Provider & API Key Management (`pkg/auth/provider_config.go`, `server.go`, `admin.go`, `llm.go`):
    *   **OrgProviderConfig Model & Encrypted Storage**: Created database schema for storing org-specific LLM provider name and API key. Encrypted keys using AES-256-GCM with a master secret environment override.
    *   **Admin Provider Configuration Endpoint**: Added `PUT /admin/orgs/{orgID}/provider` to set and encrypt the LLM provider configuration.
    *   **Dynamic Engine Model Customization**: Updated `AnthropicProvider` to allow customizing `actorModel` and `criticModel` separately. The engine resolves org provider configs and falls back to standard developer tunnel resolution.
*   **Phase 16 (Completed)**: Dashboard Multi-Tenancy (`web/index.html`, `web/app.js`, `web/style.css`, `pkg/auth/admin.go`):
    *   **Login Overlay & Credential Check**: Replaced the settings popover with a fullscreen login form overlay. The dashboard validates credentials against `/auth/validate` to verify identity and scopes tasks.
    *   **Monthly Budget Progress Bar**: Integrated real-time monthly cost totals and progress bars utilizing the `/usage` endpoint.
    *   **Interactive Admin Console**: Added tabbed administration interface for listing/creating organizations, managing organization users, generating/revoking API keys, and setting custom encrypted Anthropic LLM provider configurations.
*   **Phase 17 (Completed)**: Audit Logging & Security Hardening (`pkg/audit/audit.go`, `server.go`, `admin.go`, `db.go`):
    *   **Audit Logger Service**: Introduced `AuditLog` model schema and a thread-safe `audit` logging API with client IP, action, resource, and organization tracking.
    *   **Endpoint Instrumentation**: Added audit logs for task submissions, background executions, organization changes, user registrations, and API key updates.
    *   **Rate Limiting & CORS Hardening**: Built a thread-safe token bucket rate limiter middleware to mitigate DDoS/brute-force attacks. Tightened CORS by supporting custom origin filters via `KIWI_CORS_ALLOWED_ORIGINS` and adding preflight method authorization.
*   **Phase 18 (Completed)**: Actor–Critic Loop Observability (`events.go`, `engine.go`, `server.go`, `pkg/provider`):
    *   **Structured Per-Phase Telemetry**: Each loop phase (`initial_test`, `actor`, `critic`, `test`) emits a `TaskEvent` via an additive `Engine.EventCallback` — capturing step, duration, outcome, a truncated detail, and (Anthropic mode) input/output tokens + USD cost. Emission is best-effort and never alters loop behavior; the freeform `logs` transcript is retained.
    *   **Token Reporting**: Added `provider.TokenReporter` (`LastUsage`) implemented by `AnthropicProvider`, so cost/token attribution is per-call.
    *   **Persistence & API**: Events persist to a new `task_events` table (stamped with `OrgID`) and are served at `GET /tasks/{id}/events`, authorized via the parent task (same-org or admin) exactly like task status.

---

## 4. Current Limitations & TODOs

### Active Limitations
1.  **Single Live Provider**: The live LLM path (`pkg/provider/llm.go`) integrates Anthropic (Claude) only. OpenAI and dynamic per-role model swapping (different model for Actor vs Critic) are not yet wired up. The rule-based mock (`pkg/provider/mock.go`) remains the default (`KIWI_LLM_PROVIDER` unset).
2.  **Local Secrets Store**: The CLI client reads local secrets from an unencrypted `secrets.json` file in the workspace or defaults to standard environment variables.
3.  **Local Host Mounted Sandboxes**: The Docker sandbox mode mounts directories from the host server (`/var/folders/...`) directly into the container. In multi-tenant systems, this leaks file descriptors and permissions, requiring independent Virtual Machine sandboxing.
4.  **In-Memory Tunnel Cache**: The credentials cache on the server is kept inside the local daemon's memory. If the daemon restarts, cached credentials are lost — but as of Phase 9 the *task itself* is recovered: on boot the daemon re-launches interrupted tasks whose sandbox survived, and the re-launched loop statefully pauses until the developer reconnects the tunnel (re-populating the cache). Only the cache is ephemeral, not the task.

### Remaining Work / TODOs

#### 1. Integration of Live LLM Providers (`pkg/provider/llm.go`)
*   `[x]` Add Anthropic Go SDK client (`claude-opus-4-8`, adaptive thinking). *(OpenAI still pending.)*
*   `[x]` Create prompt templates for the **Actor** (resolves compiler/test failures with minimal edits) and the **Critic** (reviews diffs for correctness and safety before apply).
*   `[ ]` Support dynamic model swapping (e.g., a cheaper model for Actor, a stronger one for Critic).

#### 2. PKCE-based OAuth 2.0 Auth Flow
*   `[ ]` Implement a `kiwi login` command in the CLI. Spawns a temporary local HTTP server, opens the browser to authenticate via Auth0/Okta, and obtains JWT tokens.
*   `[ ]` Secure JWT storage on the developer's client machine using platform-specific keychains (e.g., `keyring` in Linux, Keychain in macOS).
*   `[ ]` Integrate JWT signature verification middleware on the server to replace the static token checks.

#### 3. Interactive Web Dashboard Controls (`web/app.js`)
*   `[ ]` Expose task cancellation (`POST /tasks/{id}/cancel`) and manual pausing (`POST /tasks/{id}/pause`) HTTP endpoints.
*   `[ ]` Update the Kanban UI with buttons to:
    *   Pause/Resume execution loops.
    *   Trigger new task submissions via a modal.
    *   Interact with human-in-the-loop checks (e.g., prompt for permission to apply a critical code fix).

#### 4. Sandbox Virtualization & Isolation Manager
*   `[ ]` Move from simple Docker mounts to a dedicated MicroVM orchestration layer (e.g. **Firecracker** or **gVisor**).
*   `[ ]` Implement a Sandbox Manager service that dynamically provisions clean kernel-isolated sandboxes in <100ms.
*   `[ ]` Support custom execution base images mapped per programming language (Go, Python, Node.js).

#### 5. High-Performance Log Streaming
*   `[ ]` Move live streaming logs from SQLite/GORM updates to **Redis Streams** or WebSocket events.
*   `[ ]` Save archived logs to an object store (e.g. AWS S3, Google Cloud Storage) upon task completion to keep the relational database clean and scalable.
