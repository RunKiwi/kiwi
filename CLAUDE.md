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
# Start the central cloud daemon listening on port 8080
./kiwid -addr :8080

# Execute a TDD auto-fixing task in Cloud Mode (default)
./kiwi -task "Fix division by zero in Divide()" -file demo_project/math_utils.go -test-cmd "go test ./demo_project/..." -server "http://localhost:8080"

# Resume a paused headless task (after a laptop-close tunnel drop)
./kiwi -resume -task-id <task_id> -server "http://localhost:8080"

# Execute a task locally (no cloud daemon, no tunnel, no sync)
./kiwi -local -task "Fix division by zero in Divide()" -file demo_project/math_utils.go -test-cmd "go test ./demo_project/..."
```

### Testing the Packages
```bash
# Run unit tests (CGO_ENABLED=0 bypasses macOS dyld UUID checks for tests)
CGO_ENABLED=0 go test -v ./pkg/...
```

---

## 2. Codebase Overview

```
kiwi/
├── cmd/
│   ├── kiwi/
│   │   └── main.go          # CLI client (zips codebase, uploads, polls logs, downloads fixes)
│   └── kiwid/
│       └── main.go          # Cloud Daemon entrypoint (starts HTTP/JSON orchestrator server)
├── pkg/
│   ├── orchestrator/
│   │   ├── engine.go        # Actor-Critic feedback loop orchestrator (tdd engine, safety gates)
│   │   └── server.go        # HTTP tasks execution controller (/tasks, /tunnel)
│   ├── sandbox/
│   │   ├── exec.go          # Shell execution command runner with env injection
│   │   └── sync.go          # Workspace zip/unzip package with Zip Slip protection
│   ├── provider/
│   │   └── mock.go          # Offline simulated LLM Actor-Critic rules
│   ├── tunnel/
│   │   └── tunnel.go        # Reverse credential tunnel stream multiplexer
│   └── dashboard/
│       └── dashboard.go     # Embedded HTML/JS Kanban board UI
├── demo_project/            # A buggy Go project used for verification testing
└── go.mod
```

---

## 3. Current Implementation Status

*   **Phase 1 (Completed)**: Core local Actor-Critic Loop Engine that corrects local compilation/test failures using LLM edit mock rules.
*   **Phase 2 (Completed)**: Cloud Sandbox & Workspace Sync. Zips local codebase, uploads it to the daemon, runs execution loops inside an isolated temp sandbox directory, and syncs fixes back.
*   **Phase 3 (Completed)**: Reverse Credential Tunneling. Relays developer credentials (`GITHUB_TOKEN`) on-demand from local client to cloud sandbox. Handles laptop close connection drops by statefully pausing the loop (`PAUSED` state) and resuming via `kiwi -resume`.
*   **Phase 4 (Completed)**: Embedded Kanban Dashboard, Cost Budgets, & Loop Safety:
    *   Sleek dark-mode dashboard hosted at `http://localhost:8080/dashboard`.
    *   Semantic duplicate-error checker halts loop if compile error occurs 3 times.
    *   Cost capping stops execution if task exceeds `$0.20` budget cap.

---

## 4. Current Limitations & TODOs

### Active Limitations
1.  **Mock AI Provider**: The LLM interface (`pkg/provider/mock.go`) uses rule-based simulations. Real integrations with Anthropic/OpenAI APIs are not yet wired up.
2.  **Shared Sandbox Isolation**: The server currently creates temporary directories (`os.MkdirTemp`) for task execution. In a production cloud setting, this must run inside isolated virtual enclaves (e.g. Docker, Firecracker, or E2B sandboxes) to prevent malicious code execution.
3.  **Local Secrets Store**: The CLI client reads local secrets from an unencrypted `secrets.json` file in the workspace or defaults to standard environment variables.

### Remaining Work / TODOs
*   `[ ]` **Wire Live Providers**: Add OpenAI and Anthropic SDK API calls to `pkg/provider/llm.go`, resolving api base and proxy tokens correctly.
*   `[ ]` **Virtual Sandboxing**: Transition `pkg/sandbox/exec.go` to dispatch commands inside a Docker container using the Docker SDK instead of running them on the host.
*   `[ ]` **SSO & Federated Auth**: Replace raw task ID generation with OIDC-based SSO tokens (Auth0, Okta) to authenticate developers to the Kiwi Cloud Daemon and delegate Vault secrets.
*   `[ ]` **Dashboard Triggering**: Allow launching new tasks and creating files directly from the Kanban Board UI instead of starting from the CLI.
