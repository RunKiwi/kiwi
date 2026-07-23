<p align="center">
  <img src="docs/assets/kiwi-logo.svg" width="88" alt="Kiwi logo" />
</p>

# Kiwi

**Kiwi turns a task into a swarm of coding agents that fix your code and open a pull request.**

A SaaS **Control Plane** decomposes a task into a DAG of workers. A **Data Plane** runs each worker in an isolated sandbox through an **Actor–Critic loop** — editing files and re-running your test command until it passes — then opens a PR. Run it **managed** (Kiwi operates the execution) or **BYOC** (the Data Plane runs in your own cloud, where code and credentials never leave your VPC).

The differentiation is the layer above the sandbox: **the planner and the swarm**, not the sandbox itself.

> [!NOTE]
> **Live, still maturing.** A task flows **end-to-end** — submit one and get a real PR back (`make local`, below). The self-serve **Free tier is deployed to production**: a signup runs tasks on a Kiwi-operated **shared fleet** without contacting us (per-org daemon processes, gVisor sandbox, agent-minute metering), served from a Cloud Run control plane plus a Docker + gVisor free-fleet host (see [Deployment](#free-tier-deployment)). Still in progress: billing / **Pro** upgrade, hardened multi-tenant **egress** isolation, and the Firecracker managed-*dedicated* path.

## Quickstart

One command brings up the whole platform — Control Plane in Docker plus a Data Plane daemon on your host. Put provider keys in `deploy/.env` and it runs real tasks immediately:

```bash
make local          # Control Plane + daemon; prints the URLs and admin token
make local-down     # stop         (make local-clean wipes the database)
```

Then submit a task (see [the CLI](#2-use-the-kiwi-cli)) or open the dashboard. To bring up the full production stack (Postgres + Control Plane + Caddy TLS + containerized daemon) on a single box, use `make prod` (requires a filled `deploy/.env`; see [`deploy/`](deploy/)).

## How it works

- **Control Plane** (`cmd/kiwid`, `pkg/orchestrator`): API + auth, a planner that decomposes a task into a DAG of `worker-spec` payloads, a Postgres **lease queue** (`pkg/store/queue.go`) that releases a worker only once its DAG dependencies have succeeded, and encrypted credential storage. Runs as split roles (`-role api | orchestrator | migrate | all`).
- **Data Plane** (`cmd/kiwidaemon`, `pkg/daemon`): a pull-model daemon that polls the Control Plane over HTTPS, opens its org's sealed credentials in memory, provisions instant workspaces via `git worktree` from a cached bare clone, and runs the Actor–Critic loop (`pkg/loop`). It can **discover the target file(s)** from the task and **edit multiple files**, so a task needs only a description and a repo.
- **Isolation**: the LLM Actor/Critic run **in the daemon process**; only the test command runs in the sandbox, so model-generated code executes with **default-deny networking** and never sees the LLM key. The sandbox driver is pluggable (`pkg/sandbox`) — Docker for dev/BYOC, **gVisor (`runsc`) for the shared Free tier** (set per-daemon via `KIWI_SANDBOX_RUNTIME=runsc`), and a Firecracker microVM driver for hardened managed execution (`KIWI_SANDBOX=firecracker`).
- **Tiers**: a **Free** tier runs every signup on a Kiwi-operated **shared fleet** — one lightweight daemon *process* per org (its own keypair, so the credential-sealing model is unchanged), packed onto shared hosts and scaled to zero when idle. Usage is bounded by per-org limits: one concurrent job, a per-task wall-clock cap, and a monthly **agent-minute** ceiling; a cryptomining heuristic auto-suspends abusive orgs. A per-org daemon is cold-started on submit by the **provisioner** (`pkg/provisioner`), which consumes `ProvisioningRequest`s and launches per-org `kiwidaemon` containers. **Pro** graduates to a dedicated fleet (managed-dedicated or BYOC).
- **Credentials**: the daemon generates an X25519 keypair (credential sealing) and an Ed25519 keypair (heartbeat signing) on boot. Customer credentials are stored by the SaaS **sealed to the daemon's X25519 public key**, and at rest are encrypted via the configured key manager (a static key for dev/BYOC, **Cloud KMS envelope encryption** for managed — `pkg/crypto`).
- **Surfaces**: the `kiwi` CLI, a Next.js **dashboard** (`frontend/` — jobs, fleets, models, integrations, live topology, settings), Node/Python SDKs, and a Linear webhook receiver.

> **Zero-knowledge is a BYOC property, not a managed one.** In BYOC the daemon runs in the customer's cloud and the Control Plane never sees plaintext credentials. In **managed**, Kiwi operates the daemon and holds the private key, so it *can* decrypt — **managed is not zero-knowledge**.

## Status

| Area | State |
| :--- | :--- |
| End-to-end seam — plan → lease → sandbox Actor–Critic loop → PR | ✅ Works ([#115](https://github.com/RunKiwi/kiwi/issues/115)) |
| One-command local / single-box prod (`make local` / `make prod`) | ✅ |
| Dashboard — jobs, fleets, models, integrations, topology, settings | ✅ |
| Multi-file agent — file discovery + multi-file edits | ✅ |
| Provider robustness — key validation on save, quota/error surfacing | ✅ |
| Fleet routing — tasks lease only their fleet's daemons | ✅ |
| Integration layer — `kiwi` CLI, Node/Python SDKs, Linear webhook | ✅ |
| **Free tier — live in production** (`app.runkiwi.dev`): per-org daemon provisioner, gVisor sandbox, agent-minute metering & abuse suspend | ✅ Deployed — Cloud Run control plane + Docker/gVisor free-fleet host (see [Deployment](#free-tier-deployment)) |
| Control plane on GCP — Cloud Run (`kiwi-api`/`kiwi-orchestrator`/`kiwi-frontend`), Cloud SQL, KMS, OAuth sign-in | ✅ Deployed |
| Self-serve signup & tenancy (GitHub/Google OAuth, per-org isolation) | ✅ Signup path live; billing / **Pro** upgrade still proposed |
| Managed-**dedicated** (Pro) — per-org VM Terraform (`deploy/gcp/`), KMS envelope crypto, Firecracker driver | 🚧 Built; not yet deployed or hardware-validated |
| Hardened multi-tenant egress isolation (default-deny on the free-fleet host) | 🚧 In progress |

## Building

`make local` builds and runs everything. To build individual binaries manually — note that newer macOS `dyld` requires external linking and an ad-hoc signature:

```bash
go build -ldflags="-linkmode=external" -o kiwi        cmd/kiwi/main.go        && codesign -s - -f ./kiwi         # CLI
go build -ldflags="-linkmode=external" -o kiwid       cmd/kiwid/main.go       && codesign -s - -f ./kiwid        # Control Plane
go build -ldflags="-linkmode=external" -o kiwidaemon  cmd/kiwidaemon/main.go  && codesign -s - -f ./kiwidaemon   # Data Plane daemon
```

## Running (manual)

`make local` does all of this for you; the manual steps are below for reference.

### 1. Start the Control Plane

Requires Postgres. NATS is optional — the Control Plane degrades with a warning if it is unreachable.

```bash
export KIWI_SERVER_TOKEN="my-secret-token-1234"
./kiwid -addr :8080 -dsn "host=localhost user=postgres password=postgres dbname=kiwi port=5432 sslmode=disable"
```

Flags: `-addr`, `-dsn`, `-role` (`api` | `orchestrator` | `migrate` | `all`), `-nats`. `-role migrate` applies migrations and exits (run it before rolling serving instances). Health checks: `/healthz` (liveness) and `/readyz` (DB-checked readiness).

### 2. Use the `kiwi` CLI

```bash
# Store your API token in ~/.config/kiwi/config.json
./kiwi login -token "my-secret-token-1234"

# Store credentials for the daemon to use (held daemon-side, never in the sandbox)
./kiwi creds set anthropic "sk-ant-..."   # or: ./kiwi creds set gemini "AI..."
./kiwi creds set git "github_pat_..."

# Submit a task. The agent can discover the file(s) and infer the test command,
# so via the API/dashboard only the task and repo are required. The CLI still
# asks for -file and -test-cmd:
./kiwi submit -task "Fix the divide-by-zero panic in Divide()" \
    -repo https://github.com/you/yourrepo -ref main \
    -file math_utils.go -test-cmd "go test ./..."

# Resume an existing task
./kiwi submit -resume -task-id <task-id>

# Launch Claude Code wrapped with Kiwi Swarm offloading instructions
./kiwi claude
```

`kiwi submit` resolves the token from `-token`, then `KIWI_SERVER_TOKEN`, then the saved login config. Use `-server` to target a non-local Control Plane and `-idempotency-key` to dedupe retried submissions.

**LLM providers.** The daemon selects the provider from the worker's `-model`: a `gemini-*` model (e.g. `-model gemini-flash-latest`) uses the stored `GEMINI_API_KEY`; any other model uses `ANTHROPIC_API_KEY`. If a task fails because a key is missing, invalid, or out of credits, the reason is surfaced on the job.

### 3. Run the Data Plane daemon

```bash
./kiwidaemon -api-url https://api.runkiwi.com \
    -key-path ~/.kiwi/daemon.key -cache-dir /tmp/kiwi-cache \
    -poll-interval 5s -max-cached-repos 20 -max-steps 6 -max-budget 0.50 \
    -join-token "$KIWI_JOIN_TOKEN"
```

On first boot the daemon generates its keypairs and registers with the Control Plane using a **single-use join token** (mint one with `POST /api/v1/daemon/join-token`, or from the dashboard's Fleets page). Once registered its persisted identity key is sufficient and the token can be omitted on restart. It then heartbeat-polls for work and runs each task through the Actor–Critic loop (`-max-steps` iterations / `-max-budget` USD per task cap the loop). The git cache keeps at most `-max-cached-repos` bare clones (default 20), evicting the least-frequently-used; `0` disables the bound. For the shared Free tier, pass `-sandbox-runtime runsc` (or `KIWI_SANDBOX_RUNTIME=runsc`) so the test command runs under gVisor; the wall-clock cap per task comes from the org's `TaskTimeoutSeconds` limit.

### 4. Dashboard

```bash
KIWI_CORS_ALLOWED_ORIGINS=http://localhost:3000 ./kiwid -addr :8080 -dsn "..."
cd frontend && cp .env.local.example .env.local   # set NEXT_PUBLIC_KIWI_API_URL=http://localhost:8080
npm ci && npm run dev                               # http://localhost:3000
```

## SDKs

Minimal v1 SDKs for programmatic submission (CI/CD, Sentry auto-triage) live in `sdk/`:

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

## Linear webhook

The Control Plane exposes `POST /api/v1/webhooks/linear`. Issues labeled `kiwi` (or moved to **In Progress**) are converted into planner jobs.

## Free-tier deployment

The Free tier is **live in production**, split across two execution substrates because `kiwi-api` / `kiwi-orchestrator` run on **Cloud Run**, which cannot run the provisioner's `docker run` launches or a gVisor (`runsc`) sandbox:

1. **Control plane on Cloud Run** — `kiwi-api`, `kiwi-orchestrator`, `kiwi-frontend`, backed by Cloud SQL (private IP). Cloud Run leaves `KIWI_PROVISIONER` unset, so its orchestrator keeps only the singleton sweepers and never attempts a `docker run`.
2. **A Docker + gVisor GCE VM** ("free-fleet host", `kiwi-free-fleet`) with `runsc` registered as a Docker runtime, on the same VPC as Cloud SQL. It runs the control-plane binary with `KIWI_PROVISIONER=docker` (which starts the provisioner independently of `-role`, so the host needs no orchestrator sweepers), `KIWI_PUBLIC_API_URL=https://api.runkiwi.dev`, and `KIWI_DAEMON_IMAGE=<AR path>/kiwidaemon:latest`. The provisioner cold-starts a per-org `kiwidaemon` container on submit; the launcher bind-mounts the host `docker.sock` so each daemon's test sandbox runs as a sibling container under `runsc`.
3. The **`kiwidaemon` image** in Artifact Registry — `docker build --target kiwidaemon` (the root `Dockerfile` ships both `kiwid` and `kiwidaemon` targets).

Schema changes (`queued_tasks.started_at`, `jobs.agent_minutes`, `org_limits.max_agent_minutes_per_month`, the `fleets.type` `self-managed`→`managed` rename, and the provisioner's partial unique index) apply via the standard `kiwid -role migrate` job. **Pro** (dedicated) stays on per-org VMs.

## Operational notes

- In `production` mode, `KIWI_ENCRYPTION_KEY`, `KIWI_SERVER_TOKEN`, and `KIWI_CORS_ALLOWED_ORIGINS` must be set explicitly. For managed, set `KIWI_KMS_KEY` to use Cloud KMS envelope encryption instead of a static key.
- The `/api/v1/planner/plan` endpoint supports idempotent submissions via the `Idempotency-Key` header.
- Database migrations apply automatically on boot; in a multi-replica deployment run `kiwid -role migrate` once before serving instead (`KIWI_SKIP_BOOT_MIGRATE=true` on serving roles).

---

## Contributing & context for AI

For build/test conventions, the PR checklist, and instructions for AI assistants, see [CLAUDE.md](CLAUDE.md).

Every PR modifying the codebase must keep this README current. If no update is needed, add the `skip-readme-check` label to the PR.

## License

Licensed under the [Apache License 2.0](LICENSE). Copyright © 2026 RunKiwi.
