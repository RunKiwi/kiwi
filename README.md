# Kiwi: BYOC Agentic Execution Engine for Startups

> [!WARNING]
> **NOT PRODUCTION READY**
> The codebase is mid-pivot from an enterprise SaaS prototype to a startup-first BYOC (Bring Your Own Cloud) execution platform, as described in the [Startup-First BYOC Platform RFC](docs/rfcs/2026-07-16-startup-byoc-platform-rfc.md). Do not use this in production.

Kiwi is an autonomous cloud execution engine for fast-moving startups. A lightweight SaaS **Control Plane** plans and orchestrates work, while fleets of AI agents (**The Swarm**) execute in Docker sandboxes inside the customer's own AWS/GCP account (**Data Plane**) — code and credentials never leave the customer's VPC.

## Architecture

- **Control Plane (Kiwi SaaS)**: API gateway and auth, a Fable-powered Planner that decomposes tasks into a DAG of `worker-spec.json` payloads, a lease-based event queue, and encrypted credential storage.
- **Data Plane (`kiwidaemon`, customer VPC)**: pull-model daemon that polls the Control Plane over HTTPS, decrypts credentials in memory, provisions instant workspaces via `git worktree` from a cached bare clone, and mounts them into Docker sandboxes running the Actor-Critic execution loop.
- **Zero-knowledge credentials**: the daemon generates an X25519 keypair (credential sealing) plus an Ed25519 keypair (heartbeat signing) on boot. Customer LLM/Git credentials are stored by the SaaS only sealed to the daemon's X25519 public key — the Control Plane never sees plaintext.
- **Integrations over UI**: `kiwi` CLI, Node/Python SDKs, and headless webhook receivers (Linear) instead of a heavy dashboard.

Design docs: [BYOC RFC](docs/rfcs/2026-07-16-startup-byoc-platform-rfc.md) · [Managed Execution Tier RFC](docs/rfcs/2026-07-17-managed-execution-tier-rfc.md) · [Architecture Review](docs/design/2026-07-16-byoc-architecture-review.md) · [Phased Plan](docs/PHASED_PLAN.md) · [Architecture](docs/design/ARCHITECTURE.md)

Positioning, strategy, and market research live in [RunKiwi/gtm](https://github.com/RunKiwi/gtm). This repo holds engineering docs only.

## Implementation Status

| Phase | Scope | Status |
| :--- | :--- | :--- |
| 1. Data Plane Foundation | `cmd/kiwidaemon`, X25519/Ed25519 crypto, heartbeat polling, `git worktree` cache, sandbox mounting | ⚠️ Components built, not connected ([#115](https://github.com/RunKiwi/kiwi/issues/115)) |
| 2. Control Plane Adaptations | Lease-based work queue, encrypted credential storage, Planner API | ⚠️ Components built, not connected ([#115](https://github.com/RunKiwi/kiwi/issues/115)) |
| 3. Integration Layer | `kiwi` CLI (`login`, `submit`, `claude`), Node/Python SDKs, Linear webhook receiver | ✅ Complete |
| 4. Distribution | Terraform/CloudFormation 1-click deploy templates | 🔜 Pending |
| M. Managed Execution Tier | Kiwi-operated Data Plane; managed as default entry, BYOC as graduation | 📋 Proposed ([RFC](docs/rfcs/2026-07-17-managed-execution-tier-rfc.md)) |

> **On Phases 1–2:** the individual components are implemented and tested, but the Data Plane and Control Plane have never been connected — no `/api/v1/daemon/heartbeat` handler exists, so a real `kiwidaemon` cannot receive work. No task flows end-to-end today. See [#115](https://github.com/RunKiwi/kiwi/issues/115).

## Building

Because of the newer macOS `dyld` dynamic linker requirements, Go binaries must be compiled with external linking and ad-hoc signed:

```bash
# CLI client
go build -ldflags="-linkmode=external" -o kiwi cmd/kiwi/main.go && codesign -s - -f ./kiwi

# Control Plane daemon (prototype monolith)
go build -ldflags="-linkmode=external" -o kiwid cmd/kiwid/main.go && codesign -s - -f ./kiwid

# BYOC Data Plane daemon
go build -ldflags="-linkmode=external" -o kiwidaemon cmd/kiwidaemon/main.go && codesign -s - -f ./kiwidaemon
```

## Running

### 1. Start the Control Plane daemon

Requires Postgres. NATS is optional — the daemon degrades with a warning if it is unreachable.

```bash
export USE_DOCKER="true"
export KIWI_SERVER_TOKEN="my-secret-token-1234"
./kiwid -addr :8080 -dsn "host=localhost user=postgres password=postgres dbname=kiwi port=5432 sslmode=disable"
```

Flags: `-addr`, `-dsn`, `-role` (`api` | `orchestrator` | `all`), `-nats`. Or bring the whole stack up with `make run-local`.

### 2. Use the `kiwi` CLI

```bash
# Store your API token in ~/.config/kiwi/config.json
./kiwi login -token "my-secret-token-1234"

# Submit a task (packages -dir, uploads it, streams logs until completion)
./kiwi submit -task "Fix division by zero in Divide()" \
    -file demo_project/math_utils.go \
    -test-cmd "go test ./demo_project/..." \
    -dir .

# Resume an existing task
./kiwi submit -resume -task-id <task-id>

# Launch Claude Code wrapped with Kiwi Swarm offloading instructions
./kiwi claude
```

`kiwi submit` resolves the token from `-token`, then `KIWI_SERVER_TOKEN`, then the saved login config. Use `-server` to target a non-local Control Plane and `-idempotency-key` to dedupe retried submissions.

### 3. Run the BYOC daemon (Data Plane)

```bash
./kiwidaemon -api-url https://api.runkiwi.com \
    -key-path ~/.kiwi/daemon.key \
    -poll-interval 5s \
    -cache-dir /tmp/kiwi-cache
```

On first boot the daemon generates its keypairs, self-registers with the Control Plane, and begins heartbeat polling for `worker-spec.json` payloads.

### 4. Dashboard

```bash
cd frontend && npm install && npm run dev
```

## SDKs

Minimal v1 SDKs for programmatic task submission (CI/CD, Sentry auto-triage) live in `sdk/`:

```js
// Node (sdk/node)
const { KiwiClient } = require('kiwi-sdk');
const client = new KiwiClient('http://localhost:8080', process.env.KIWI_TOKEN);
await client.submitTask('Fix flaky test', 'pkg/foo/foo.go', 'go test ./...', './codebase.zip');
```

```python
# Python (sdk/python)
from kiwi import KiwiClient
client = KiwiClient("http://localhost:8080", token)
client.submit_task("Fix flaky test", "pkg/foo/foo.go", "go test ./...", "./codebase.zip")
```

## Linear Webhook

The Control Plane exposes `POST /api/v1/webhooks/linear`. Issues labeled `kiwi` (or moved to **In Progress**) are automatically converted into planner jobs.

---

## Contributing & Context for AI

For system context, PR checklist, and instructions for AI assistants, see [CLAUDE.md](CLAUDE.md) and the docs inside `docs/`.

Every PR modifying the codebase must also keep this README updated. If no update is necessary, add the `skip-readme-check` label to the PR.