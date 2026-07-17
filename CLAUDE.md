# Kiwi Development Guide (CLAUDE.md)

This file provides system context, architecture guidelines, and current status for AI assistants and contributors working on the **Kiwi** codebase.

---

## 1. Project State & Architecture

**Read this first:** the individual components are built and tested, but **no task flows end-to-end.** The Data Plane and Control Plane have never been connected — the daemon polls `/api/v1/daemon/heartbeat`, which no server registers; `LeaseNextTask` has no production caller; `crypto.OpenSealed` is never called; and the daemon runs a placeholder `echo` instead of an agent. See [#115](https://github.com/RunKiwi/kiwi/issues/115). Do not assume a code path works because its package tests pass — `pkg/daemon` tests against an `httptest` mock of a server that does not exist.

**Current State**: Three architectures coexist in the tree, and the pivot has not reconciled them:
1. **v0 prototype** — monolithic daemon (`kiwid`) + CLI (`kiwi`), SQLite-backed auth.
2. **Abandoned enterprise build-out** — `pkg/tunnel`, `pkg/billing`, `pkg/audit`, `pkg/agentapi`, `pkg/checkpoint`. Paused by the BYOC pivot; still compiled and tested.
3. **Current BYOC direction** — `cmd/kiwidaemon`, `pkg/daemon`, `pkg/crypto`, `pkg/gitcache`, `pkg/planner`, and the lease queue in `pkg/store`.

**It is not production-ready.**

**Target State**: A **BYOC (Bring Your Own Cloud) agentic execution platform** — a SaaS Control Plane that plans and orchestrates, with the Data Plane executing in the customer's cloud. A **managed tier** (Kiwi-operated Data Plane) is proposed alongside it. Read these before making architectural decisions or creating new modules:

- [RFC: Startup-First BYOC Platform Pivot](docs/rfcs/2026-07-16-startup-byoc-platform-rfc.md) — the architecture of record
- [RFC: Managed Execution Tier](docs/rfcs/2026-07-17-managed-execution-tier-rfc.md) — proposed; managed as entry, BYOC as graduation
- [Architecture Review](docs/design/2026-07-16-byoc-architecture-review.md) — known gaps and unresolved contradictions
- [Phased Plan](docs/PHASED_PLAN.md) · [Architecture](docs/design/ARCHITECTURE.md)

Positioning, strategy, and market research live in **[RunKiwi/gtm](https://github.com/RunKiwi/gtm)**, not here. This repo holds engineering docs only.

### Key Architectural Constraints for New Code:
- **Language**: Go (`go.mod` targets 1.25).
- **Persistence**: **PostgreSQL** via GORM. Use strong consistency (transactional outbox) for state transitions. `migrations/0001` is the intended source of truth, but note it has drifted — `queued_tasks` and `credentials` exist only via `AutoMigrate` in `pkg/orchestrator/db.go`.
- **Queue**: **NATS JetStream** for durable queuing/event streaming; the BYOC daemon handoff uses the Postgres **lease queue** (`pkg/store/queue.go`) — tasks are leased, not popped, so a crashed daemon's work returns to the queue.
- **Multi-tenant**: Every task-scoped row carries `org_id`.
- **Security**: Secrets are never persisted in the sandbox. Customer credentials are sealed to the daemon's X25519 public key (`pkg/crypto`). **The JIT reverse-tunnel broker is paused** — it was an enterprise-era feature dropped in the pivot. `pkg/tunnel` remains in-tree but is not the direction; the plan is a local credential-injecting egress proxy (see Architecture Review §3.1).
- **Zero-knowledge is a BYOC-only claim.** Under the proposed managed tier, Kiwi operates the machine holding the private key and *can* decrypt. Do not write docs or code comments claiming zero-knowledge for managed mode.
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

# BYOC daemon (currently polls an endpoint the Control Plane does not serve — see #115)
./kiwidaemon -api-url http://localhost:8080 -key-path ~/.kiwi/daemon.key
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
├── docs/                 # RFCs and design docs
└── demo_project/         # A buggy Go project used for verification testing
```
