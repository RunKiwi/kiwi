# Kiwi: Distributed Agentic Execution Platform

> [!WARNING]
> **NOT PRODUCTION READY**
> The current codebase (`main`) is an early prototype (v0 monolith). We are actively re-architecting the system into a Distributed Agentic Execution Platform as described in our [target-state RFC](docs/rfcs/2026-07-10-agentic-execution-platform-rfc.md). Do not use this in production.

Kiwi is an enterprise-ready secure execution engine that runs autonomous developer loops in the cloud while pulling required secrets dynamically and securely from the local machine over a reverse tunnel or natively via cloud OIDC.

## The Vision: Target Architecture
We are transitioning to a distributed, strongly-consistent control plane to orchestrate LLM agent workflows inside isolated sandboxes. 
Key features of the target architecture include:
- **Control Plane & Sandbox Separation**: Stateless API servers, a PostgreSQL source of truth, and NATS JetStream for durable task queues.
- **Idempotency & Resiliency**: Checkpoint/rollback mechanisms using S3 workspace snapshots, a side-effect ledger to prevent double-firing effects, and automatic recovery of interrupted runs.
- **Master-Worker Topology**: Complex workflows driven by a Master agent coordinating multiple sub-task Workers within the sandbox.
- **Pluggable Infrastructure & BYO-LLM**: Choose your execution isolation (Docker, gVisor, Firecracker, Kubernetes) and your LLM provider (Anthropic, Codex, Gemini, or any compatible endpoint).
- **Bring-Your-Own-Cloud (BYOC)**: A SaaS control plane with remote `kiwi-runner` daemons deployed directly in customer VPCs.

For detailed plans, please see the [RFCs & Implementation Plans](docs/).

---

## The Prototype (Current State)

The code currently in `main` represents the **Phase 1 Prototype**. It operates as a monolithic daemon (`kiwid`) with local SQLite persistence and a CLI client (`kiwi`).

### Core Prototype Features
1. **Actor-Critic Alignment Loop**: A test-driven development (TDD) controller that iteratively edits code, evaluates compiler/test stdout in a local Docker sandbox, and refines fixes.
2. **Reverse Credential Tunneling**: Pulls temporary credentials (e.g., `GITHUB_TOKEN`) locally and passes them to the sandbox on-demand.
3. **Interactive Kanban Dashboard**: Embedded dark-themed dashboard featuring status cards, live polling, log filters, and a real-time console log viewer.

### Quick Setup (Prototype)

1. **Clone & Build**:
    ```bash
    go build -ldflags="-linkmode=external" -o kiwi cmd/kiwi/main.go && codesign -s - -f ./kiwi
    go build -ldflags="-linkmode=external" -o kiwid cmd/kiwid/main.go && codesign -s - -f ./kiwid
    ```

2. **Start the Kiwi Server Daemon**:
    ```bash
    export USE_DOCKER="true"
    export KIWI_SERVER_TOKEN="my-secret-token-1234"
    ./kiwid -addr :8080 -db kiwi.db
    ```

3. **Deploy a Task**:
    Set up a local `secrets.json` file in your workspace directory (or export env vars) and execute:
    ```bash
    echo '{"GITHUB_TOKEN": "real-token-value-here"}' > secrets.json
    ./kiwi -token "my-secret-token-1234" -task "Fix division by zero" -file demo_project/math_utils.go -test-cmd "go test ./demo_project/..."
    ```

4. **Access the Dashboard**:
    ```bash
    npx -y serve -l 3000 web/
    ```

---

## Contributing & Context for AI
For system context, PR checklist, and instructions for AI assistants, please refer to [CLAUDE.md](CLAUDE.md) and the plans inside `docs/plans/`.

Every PR modifying the codebase must also keep this README updated. If no update is necessary, add the `skip-readme-check` label to the PR.
