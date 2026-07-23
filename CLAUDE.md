# Kiwi Development Guide (CLAUDE.md)

This file provides system context, architecture guidelines, and current status for AI assistants and contributors working on the **Kiwi** codebase.

---

## 1. Project State & Architecture

Kiwi is a **BYOC (Bring Your Own Cloud) agentic execution platform**: a **Control Plane** plans a task into a worker DAG and hands work out via a Postgres lease queue; a **Data Plane** daemon leases a worker, runs an **Actor–Critic loop** in a sandbox until the test command passes, and opens a PR. Run **managed** (Kiwi operates the Data Plane) or **BYOC** (the Data Plane runs in the customer's cloud).

**What's live:** the full path works end-to-end. A task posted to `/api/v1/planner/plan` is planned, leased by a per-org daemon, run through the loop with the test command in a **gVisor** sandbox, and returned as a PR. A self-serve **Free tier** runs every signup on a Kiwi-operated **shared fleet** — a per-org `kiwidaemon` process cold-started on submit by the **provisioner** (`pkg/provisioner`), gVisor-sandboxed, agent-minute-metered, with rolling-window abuse auto-suspend.

**Active vs. dormant packages** — not everything in the tree is on the live path:
- **Active:** `cmd/kiwid`, `cmd/kiwidaemon`, `pkg/orchestrator`, `pkg/daemon`, `pkg/planner`, `pkg/provisioner`, `pkg/store`, `pkg/crypto`, `pkg/gitcache`, `pkg/sandbox`, `pkg/auth`.
- **Dormant** — compiled and tested, but not on the active path; don't build on them without discussion: `pkg/tunnel`, `pkg/billing`, `pkg/audit`, `pkg/agentapi`, `pkg/checkpoint`.

**In progress:** billing / **Pro** upgrade (UI-stubbed, no checkout wired), hardened multi-tenant **egress** isolation on the free-fleet host, and a **Firecracker** managed-*dedicated* path (built, not deployed or hardware-validated).

### Key Architectural Constraints for New Code:
- **Language**: Go (`go.mod` targets 1.25).
- **Persistence**: **PostgreSQL** via GORM. Use strong consistency (transactional outbox) for state transitions. Numbered `migrations/*.up.sql` files are applied by `RunMigrations` (`pkg/orchestrator/migrate.go`), tracked in `schema_migrations`, and run via `kiwid -role migrate` (prod serving roles set `KIWI_SKIP_BOOT_MIGRATE=true`). Note the schema has drifted from `migrations/0001` — some tables (e.g. `queued_tasks`, `credentials`) exist only via `AutoMigrate` in `pkg/orchestrator/db.go`. Add data/DDL changes as a new numbered migration.
- **Queue**: **NATS JetStream** for durable queuing/event streaming; the BYOC daemon handoff uses the Postgres **lease queue** (`pkg/store/queue.go`) — tasks are leased, not popped, so a crashed daemon's work returns to the queue.
- **Multi-tenant**: Every task-scoped row carries `org_id`.
- **Security**: Secrets are never persisted in the sandbox. Customer credentials are sealed to the daemon's X25519 public key (`pkg/crypto`); the LLM Actor/Critic run in the daemon process, and only the test command runs in the sandbox (default-deny networking), so model-generated code never sees the keys.
- **Zero-knowledge is a BYOC-only claim.** In managed mode Kiwi operates the machine holding the private key and *can* decrypt. Do not write docs or code comments claiming zero-knowledge for managed mode.
- **Terminology**: The supported LLM providers are Anthropic, Codex, Gemini, or compatible endpoints. **Do not use the literal three-letter "Open"+"AI" brand string in code, config, or docs (use `codex` instead).**

---

## 2. Mandatory Pre-Commit Checks
CI enforces these on every PR. Run them locally **before every commit**:
```bash
gofmt -l cmd/ pkg/                 # MUST print nothing. Fix with: gofmt -w cmd/ pkg/
CGO_ENABLED=0 go vet ./...         # MUST be clean
CGO_ENABLED=0 go test ./pkg/...    # MUST pass
CGO_ENABLED=0 go build ./...       # MUST build all packages
```
Treat any failure as a hard blocker.

---

## 3. Pull Request Requirements
- **Tests**: Every new feature must ship with tests first. Use stubs for providers/infrastructure in CI.
- **Documentation**: The `README.md` file must be kept up-to-date with any codebase changes. A GitHub Action enforces this. If a PR does not require a README update, it must be labeled with `skip-readme-check`.

---

## 4. Compilation & Running (Prototype)

Because of the newer macOS `dyld` dynamic linker requirements, Go binaries must be compiled with external linking and ad-hoc signed:

```bash
# CLI client
go build -ldflags="-linkmode=external" -o kiwi cmd/kiwi/main.go && codesign -s - -f ./kiwi

# Control Plane daemon
go build -ldflags="-linkmode=external" -o kiwid cmd/kiwid/main.go && codesign -s - -f ./kiwid

# BYOC Data Plane daemon
go build -ldflags="-linkmode=external" -o kiwidaemon cmd/kiwidaemon/main.go && codesign -s - -f ./kiwidaemon
```

### Running the services
```bash
# Control Plane. Requires Postgres; NATS is optional (degrades with a warning).
# Flags: -addr, -dsn, -role (api|orchestrator|all), -nats. There is no -db flag.
export USE_DOCKER="true"
./kiwid -addr :8080 -dsn "host=localhost user=postgres password=postgres dbname=kiwi port=5432 sslmode=disable"

# CLI
./kiwi login -token "my-secret-token-1234"
./kiwi submit -task "Fix division by zero in Divide()" \
    -file demo_project/math_utils.go \
    -test-cmd "go test ./demo_project/..." \
    -dir .

# BYOC Data Plane daemon: registers with a single-use join token, then heartbeat-polls
# the Control Plane for leased work (the seam is live — see README).
./kiwidaemon -api-url http://localhost:8080 -key-path ~/.kiwi/daemon.key -join-token "$KIWI_JOIN_TOKEN"
```

`make run-local` brings up the Compose stack (Postgres, NATS, MinIO). The Next.js frontend lives in `frontend/`.

---

## 5. Directory Structure Overview
```text
kiwi/
├── cmd/
│   ├── kiwi/             # CLI client (login, submit, claude)
│   ├── kiwid/            # Control Plane daemon (API + orchestrator roles)
│   ├── kiwidaemon/       # BYOC Data Plane daemon (out-bound polling)
│   └── kiwi-agent/       # In-sandbox agent entrypoint
├── pkg/
│   ├── daemon/           # BYOC daemon: heartbeat client, poll loop
│   ├── crypto/           # X25519 sealing + Ed25519 signing
│   ├── gitcache/         # Bare clone + git worktree provisioning
│   ├── planner/          # Task -> worker DAG decomposition
│   ├── provisioner/      # Free tier: consumes ProvisioningRequests, launches per-org daemon containers (docker + runsc)
│   ├── store/            # Postgres models, lease queue, sealed credentials
│   ├── queue/            # NATS JetStream relay + consumer
│   ├── orchestrator/     # Core LLM loop, engine, HTTP server, webhooks
│   ├── agent/            # In-sandbox master/worker runtime
│   ├── sandbox/          # Execution isolator (Docker v1)
│   ├── infra/            # Docker driver
│   ├── provider/         # Pluggable LLM interface (+ mock)
│   ├── auth/             # Orgs, API keys, limits
│   ├── manifest/         # Manifest generation
│   ├── client/           # Go client for the Control Plane API
│   ├── dashboard/        # Server-rendered dashboard
│   ├── agentapi/         # [paused] Master-worker API
│   ├── checkpoint/       # [paused] Event log / snapshotting
│   ├── audit/            # [paused] Audit log
│   ├── billing/          # [paused] Usage/cost
│   └── tunnel/           # [paused] Reverse credential proxy — see §1
├── frontend/             # Next.js dashboard
├── sdk/                  # Node + Python SDKs
├── migrations/           # Postgres schema (see drift note in §1)
├── docs/                 # Public assets (logo)
└── demo_project/         # A buggy Go project used for verification testing
```
