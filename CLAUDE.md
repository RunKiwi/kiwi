# Kiwi Development Guide (CLAUDE.md)

This file provides system context, architecture guidelines, and current status for AI assistants and contributors working on the **Kiwi** codebase.

---

## 1. Project State & Architecture
**Current State**: The codebase on `main` is an **early prototype** (v0) consisting of a monolithic daemon (`kiwid`) backed by SQLite and a CLI client (`kiwi`). **It is not production-ready.**

**Target State**: We are currently in the middle of re-architecting Kiwi into a **Distributed Agentic Execution Platform**. You **must** read the RFC and implementation plans before making architectural decisions or creating new modules:
- [RFC: Distributed Agentic Execution Platform](docs/rfcs/2026-07-10-agentic-execution-platform-rfc.md)
- [Implementation Plan](docs/plans/2026-07-11-agentic-platform-implementation-plan.md)
- [Deployment & BYOC Plan](docs/plans/2026-07-11-deployment-runner-plan.md)

### Key Architectural Constraints for New Code:
- **Language**: Go 1.21+
- **Persistence**: We are migrating from SQLite to **PostgreSQL**. Use strong consistency (transactional outbox) for state transitions.
- **Queue**: We are moving to **NATS JetStream** for durable queuing and event streaming.
- **Multi-tenant**: Every task-scoped row carries `org_id`.
- **Security**: Secrets are never persisted in the sandbox. Use the JIT reverse-tunnel broker.
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
# Build Client CLI
go build -ldflags="-linkmode=external" -o kiwi cmd/kiwi/main.go
codesign -s - -f ./kiwi

# Build Server Daemon
go build -ldflags="-linkmode=external" -o kiwid cmd/kiwid/main.go
codesign -s - -f ./kiwid
```

### Running the Prototype Services
```bash
# Start the central cloud daemon with SQLite persistence and Docker sandboxing enabled
export USE_DOCKER="true"
export KIWI_SERVER_TOKEN="my-secret-token-1234"
./kiwid -addr :8080 -db kiwi.db

# Execute a TDD auto-fixing task using Bearer Token authorization
./kiwi -token "my-secret-token-1234" -task "Fix division by zero in Divide()" -file demo_project/math_utils.go -test-cmd "go test ./demo_project/..."
```

### Launching the Dashboard Frontend
```bash
npx -y serve -l 3000 web/
# Configure the Kiwi Daemon URL (e.g. http://localhost:8080) and token inside the dashboard Settings gear panel in the upper-right corner.
```

---

## 5. Directory Structure Overview
```text
kiwi/
├── cmd/
│   ├── kiwi/             # CLI client prototype
│   ├── kiwid/            # Monolith Daemon prototype
│   └── kiwi-runner/      # [WIP] BYOC out-bound polling runner
├── pkg/
│   ├── store/            # [WIP] Postgres migration layer
│   ├── queue/            # [WIP] NATS integration layer
│   ├── agentapi/         # [WIP] Master-worker gRPC communication
│   ├── checkpoint/       # [WIP] S3 workspace snapshotting
│   ├── orchestrator/     # Core LLM looping & engine
│   ├── sandbox/          # Execution isolator (Docker v1)
│   ├── provider/         # Pluggable LLM interface
│   ├── tunnel/           # Reverse credential proxy
│   └── web/              # Standalone Dashboard Frontend
├── docs/                 # RFCs, design docs, implementation plans
└── demo_project/         # A buggy Go project used for verification testing
```
