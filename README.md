<p align="center">
  <img src="docs/assets/kiwi-logo.svg" width="88" alt="Kiwi logo" />
</p>

# Kiwi

**Kiwi runs coding agents inside infrastructure you control, and shows its work.**

A SaaS **Control Plane** decomposes a task into a DAG of workers. A **Data Plane** runs each worker in an isolated sandbox through an **Actor–Critic loop**, editing files and re-running your test command until it passes, then opens a PR. Run it **managed**, where Kiwi operates the execution, or **BYOC**, where the Data Plane runs in your own cloud and neither code nor credentials leave your VPC.

Two properties hold on every task, in both modes.

- **The execution is contained.** Model-generated code runs only as your test command, inside a sandbox with default-deny networking, and never sees an API key. The Actor and Critic run in the daemon process, not the sandbox.
- **The execution is on the record.** Every proposed edit, every Critic verdict and its reasons, and every test run is persisted per phase with its model, token counts, cost and duration, so you can trace a merged diff back to what produced it and what proved it.

## Try it

**[Sign up or log in at app.runkiwi.dev →](https://app.runkiwi.dev)** is the fastest way to run Kiwi, with no setup. Sign in with GitHub or Google. Every account starts on the **Free tier**, a Kiwi-operated shared fleet. Connect a repo, add your own model key, submit a task, and the swarm plans it, runs it, and opens a PR.

Prefer to run it yourself? See the self-host [Quickstart](#quickstart) below.

> [!NOTE]
> **Live, still maturing.** A task flows end to end: submit one and you get a real PR back (`make local`, below). The self-serve **Free tier runs in production**, so a signup runs tasks on a Kiwi-operated **shared fleet** without contacting us, with per-org daemon processes, a gVisor sandbox and agent-minute metering, served from a Cloud Run control plane plus a Docker + gVisor free-fleet host (see [Deployment](#free-tier-deployment)). Multi-tenant **egress isolation** is live on that host. Two things are still in build: **Pro** has no self-serve checkout, so Pro runs BYOC today and we set it up by email, and the Firecracker managed-*dedicated* path is written but not deployed.

## Quickstart

One command brings up the whole platform: Control Plane in Docker plus a Data Plane daemon on your host. Put provider keys in `deploy/.env` and it runs real tasks immediately:

```bash
make local          # Control Plane + daemon; prints the URLs and admin token
make local-down     # stop         (make local-clean wipes the database)
```

Then submit a task (see [the CLI](#2-use-the-kiwi-cli)) or open the dashboard. To bring up the full production stack (Postgres + Control Plane + Caddy TLS + containerized daemon) on a single box, use `make prod` (requires a filled `deploy/.env`; see [`deploy/`](deploy/)).

## How it works

```
you ──▶ Control Plane ──lease──▶ Data Plane daemon ──▶ your repo ──▶ PR
        plan · queue · seal      loop · sandbox · git
```

- **Control Plane** (`cmd/kiwid`, `pkg/orchestrator`): API, auth, the planner that turns one task into a DAG of `worker-spec` payloads, a Postgres **lease queue** that releases a worker only once its dependencies have succeeded, and sealed credential storage. Runs as split roles (`-role api | orchestrator | migrate | all`). It never executes your code. → [Control Plane](https://docs.runkiwi.dev/control-plane), [Planner](https://docs.runkiwi.dev/planner), [DAGs](https://docs.runkiwi.dev/dags)
- **Data Plane** (`cmd/kiwidaemon`, `pkg/daemon`): a pull-model daemon that polls over HTTPS, opens its org's credentials in memory, provisions a workspace with `git worktree` from a cached bare clone, runs the loop, and opens the PR. Outbound connections only, so it sits inside a customer VPC with no inbound firewall holes. → [Data Plane](https://docs.runkiwi.dev/data-plane)
- **Two execution loops.** **File Loop** is the default: an Actor proposes a patch, a Critic reviews it before anything reaches disk, and your test command verifies. **Session mode** (`mode: session`) is for open-ended work: an Architect sets each round's objective and reviews the diff while an Implementer works the repo with real tools. → [Session mode](https://docs.runkiwi.dev/session-mode)
- **The task is the goal; the test is a guard.** Your description is what Kiwi tries to achieve. The test command proves the change broke nothing, and is never the definition of done. A green suite is no reason to skip the work, a run that changes no code is reported as a failure, and while the suite is red the agent may not edit the failing test.
- **Isolation.** The Actor and Critic run **in the daemon process**; only your test command runs in the sandbox, so model-generated code never sees an API key. The sandbox is **two-phase**: dependencies install with a network and an empty environment, then verification runs offline. Model-generated code never has network access, and the phase that does never holds a secret. Drivers are pluggable (Docker, gVisor `runsc` for the shared Free tier, Firecracker). → [Sandbox & isolation](https://docs.runkiwi.dev/sandbox)
- **Zero setup.** No image, no test command, no file list. The daemon reads what your repository declares (`devcontainer.json`, the test command's own executable, then `go.mod` / `.nvmrc` / `.python-version`), picks the runtime, infers the test command, and resolves the planner's file hints against the real tree. A wrong runtime guess self-corrects and re-runs once before the Actor sees the error.
- **Credentials.** The daemon generates an X25519 keypair (sealing) and an Ed25519 keypair (signing) on boot. Customer credentials are stored **sealed to that daemon's public key**, encrypted at rest by the configured key manager, and opened only in the daemon's memory. `GIT_TOKEN` authenticates both the clone and the push, so private repos work; cloning happens in the daemon, never in a sandbox. → [Security & credentials](https://docs.runkiwi.dev/security)
- **Every run is on the record.** Each phase is persisted with its model, verdict, tokens, cost and duration, and streamed to the dashboard while the run is still going. When a job finishes, the Control Plane assembles a per-job **execution record**, signed by the daemon's own key and hash-chained per org. Raw output is never stored, only digests. → [Execution record](https://docs.runkiwi.dev/execution-record), [Observability](https://docs.runkiwi.dev/observability)
- **Tiers.** **Free** runs every signup on a Kiwi-operated shared fleet: one daemon process per org, cold-started on submit by the provisioner, gVisor-sandboxed, bounded by one concurrent job, a 20-minute per-task cap and a monthly agent-minute ceiling. **Pro** moves to a dedicated fleet. → [Tiers & the Free fleet](https://docs.runkiwi.dev/tiers)
- **Surfaces.** The `kiwi` CLI, a Next.js dashboard (`frontend/`), Node and Python SDKs, and Linear and GitHub webhooks.

Full documentation lives at **[docs.runkiwi.dev](https://docs.runkiwi.dev)**. What shipped when is in [Status](#status) below; why a given design is the way it is tends to be written down next to the code.

## Status

| Area | State |
| :--- | :--- |
| End-to-end seam: plan → lease → sandbox Actor–Critic loop → PR | ✅ Works ([#115](https://github.com/RunKiwi/kiwi/issues/115)) |
| One-command local / single-box prod (`make local` / `make prod`) | ✅ |
| Dashboard: jobs, fleets, models, integrations, topology, settings | ✅ |
| Multi-file agent: file discovery + multi-file edits | ✅ |
| Provider robustness: key validation on save, quota/error surfacing | ✅ |
| Fleet routing: tasks lease only their fleet's daemons | ✅ |
| Queue diagnostics: a queued task reports *why* it hasn't started | ✅ |
| Job control: stop / retry / delete, with a real abort on the daemon | ✅ |
| Fleet-host autoscaling: scale the free-fleet machine to zero when idle | ✅ (opt-in) |
| Integration layer: `kiwi` CLI, Node/Python SDKs, Linear webhook | ✅ |
| Shared context: plan with prior-job learnings (Auto pgvector search / Manual select), org-scoped, opt-in | ✅ |
| Execution record: per-job provenance, daemon-attested + CP-signed, hash-chained per org (`pkg/ver`) | ✅ Records assemble and sign; set `KIWI_VER_SIGNING_KEY` or they persist `unsigned` |
| Plan validation: reject cyclic/dangling dependencies, duplicate IDs, and undeclared file conflicts at submit time | ✅ |
| Merge provenance: GitHub PR-merge webhook appends a signed `kiwi.ver/merge/v1` link capturing the approver | ✅ Set `GITHUB_WEBHOOK_SECRET` |
| **Free tier: live in production** (`app.runkiwi.dev`): per-org daemon provisioner, gVisor sandbox, agent-minute metering & abuse suspend | ✅ Deployed: Cloud Run control plane + Docker/gVisor free-fleet host (see [Deployment](#free-tier-deployment)) |
| Control plane on GCP: Cloud Run (`kiwi-api`/`kiwi-orchestrator`/`kiwi-frontend`), Cloud SQL, KMS, OAuth sign-in | ✅ Deployed |
| Self-serve signup & tenancy (GitHub/Google OAuth, per-org isolation) | ✅ Signup path live |
| Billing: Stripe Checkout for the **Pro** upgrade + signed webhook (plan/limits) | ✅ Wired (test mode); set `STRIPE_*` env to enable, else the free path is unaffected |
| Managed-**dedicated** (Pro): per-org VM Terraform (`deploy/gcp/`), KMS envelope crypto, Firecracker driver | 🚧 Built; not yet deployed or hardware-validated |
| Egress isolation: sandbox `--network none` (enforced + tested) + host metadata-endpoint hardening (`deploy/free-fleet/`) | ✅ Shipped; apply on the fleet host |
| Session loop: a task-long Architect (plan + review) driving an agentic Implementer with real tool calls, in reviewed rounds | 🚧 Building, phase by phase: [docs/rfc-session-loop.md](docs/rfc-session-loop.md); `pkg/loop` stays the default path |
| ├ Tool-calling seam (`provider.ToolRunner`) + persistent sandbox (`sandbox.Session`) + cache-aware pricing | ✅ Phase 0 |
| ├ `pkg/session`: Architect plans/reviews, Implementer works with tools; opt-in via `spec.mode: session` | ✅ Phase 1: the sandbox gets **no credentials** in this mode (`KIWI_SESSION_ALLOW_TEST_CREDS` opts back in) |
| ├ Crash recovery: round-level checkpoints (`agent_sessions`, migration 0021); a re-leased task resumes at its last finished round | ✅ Phase 2 |
| ├ Cost: prompt caching on by default, cache-priced budgets, mid-round transcript compaction | ✅ Phase 3 |
| ├ Planner collapse: one worker per session job, **no LLM call and no credential decryption on the Control Plane** (`KIWI_SESSION_MODE=off` disables) | ✅ Phase 4 |
| └ Provider parity: tool-calling on Anthropic, Gemini and OpenAI, so session mode is not one vendor's feature | ✅ Phase 5: Gemini additionally echoes the `thoughtSignature` it requires back on replay; without it the second tool turn of every conversation is rejected |
| └ Reachable from the clients: `-mode session` on the CLI, `mode` in the SDK/API, an **Execution loop** control in the dashboard | ✅ Proven end to end in production: Architect plans, Implementer works with tools, reviewer approves, PR opened. `file_loop` stays the default everywhere and the key is omitted entirely unless session is chosen. The dashboard hides the **Plan** model chip in session mode, where it decides nothing: the Architect does the planning, inside the daemon |
| └ Partial edits: `edit_file` replaces an exact string instead of rewriting the file whole | ✅ The Implementer previously had one way to change a file: supply its complete new contents. Output tokens (and the latency that comes with them) therefore scaled with file size rather than change size, and every edit produced a diff full of reformatting for the reviewer to read. `edit_file` refuses rather than guesses: an `old_string` that is missing, ambiguous, or belongs to a file this round has not read is an error with a hint, because each quiet alternative writes a plausible-looking edit somewhere the test command may never look. `read_file` gained `offset`/`limit` and line numbers, and now truncates from the **end**: it kept the last 64KB, which is right for a failing build and wrong for source, where a model that saw only the bottom of a file then reconstructed its imports from memory |

## Building

`make local` builds and runs everything. To build individual binaries manually, note that newer macOS `dyld` requires external linking and an ad-hoc signature:

```bash
go build -ldflags="-linkmode=external" -o kiwi        cmd/kiwi/main.go        && codesign -s - -f ./kiwi         # CLI
go build -ldflags="-linkmode=external" -o kiwid       cmd/kiwid/main.go       && codesign -s - -f ./kiwid        # Control Plane
go build -ldflags="-linkmode=external" -o kiwidaemon  cmd/kiwidaemon/main.go  && codesign -s - -f ./kiwidaemon   # Data Plane daemon
```

## Running (manual)

`make local` does all of this for you; the manual steps are below for reference.

### 1. Start the Control Plane

Requires Postgres. NATS is optional, and the Control Plane degrades with a warning if it is unreachable.

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
./kiwi creds set anthropic "sk-ant-..."   # or: gemini "AI..." / openai "sk-..."
./kiwi creds set git "github_pat_..."

# Submit a task. The agent can discover the file(s) and infer the test command,
# so via the API/dashboard only the task and repo are required. The CLI still
# asks for -file and -test-cmd:
./kiwi submit -task "Fix the divide-by-zero panic in Divide()" \
    -repo https://github.com/you/yourrepo -ref main \
    -file math_utils.go -test-cmd "go test ./..."

# Run it as an agentic session instead: an Architect plans the whole task and
# reviews each round, an Implementer works with real tool calls (read, grep,
# write, run). -architect-model is optional and defaults to -model; it is worth
# setting to something more capable, since the reviewer is called a handful of
# times per task while the implementer runs constantly.
./kiwi submit -task "Add a Modulo function mirroring Divide, with table-driven tests" \
    -repo https://github.com/you/yourrepo -ref main \
    -file math_utils.go -test-cmd "go test ./..." \
    -mode session -model claude-haiku-4-5-20251001 -architect-model claude-sonnet-5

# Resume an existing task
./kiwi submit -resume -task-id <task-id>

# Launch Claude Code wrapped with Kiwi Swarm offloading instructions
./kiwi claude
```

`kiwi submit` resolves the token from `-token`, then `KIWI_SERVER_TOKEN`, then the saved login config. Use `-server` to target a non-local Control Plane and `-idempotency-key` to dedupe retried submissions.

**LLM providers.** The daemon selects the provider from the worker's `-model`, and reads that provider's key from your stored credentials:

| Model id | Provider | Credential |
| --- | --- | --- |
| `gemini-*` (e.g. `gemini-flash-latest`) | Gemini | `GEMINI_API_KEY` |
| `gpt-*`, `o1*`, `o3*`, `o4*`, `chatgpt-*` (e.g. `gpt-5-mini`) | OpenAI | `OPENAI_API_KEY` |
| anything else (e.g. `claude-opus-4-8`) | Anthropic | `ANTHROPIC_API_KEY` |

Anthropic's **adaptive thinking** is requested only for models that support it (Claude 4.6 and later, see `pkg/provider/thinking.go`). It arrived with that generation, and older models reject it outright with `400 adaptive thinking is not supported on this model` rather than ignoring the field, so sending it unconditionally broke every task on `claude-haiku-4-5`, the default worker model. Unknown models get no thinking rather than a guess: losing thinking costs quality on one call, guessing wrong fails the task.

The same rule decides which key the planner uses, how a call is priced, and which provider the signed execution record names. It is one function (`provider.ProviderOf`) rather than a rule repeated per component. If a task fails because a key is missing, invalid, or out of credits, the reason is surfaced on the job.

**Transient provider failures are retried** (`pkg/provider/retry.go`): `429` and `5xx` are retried with exponential backoff and jitter, honouring the provider's own `Retry-After` when it sends one, and never sleeping past the caller's deadline. This matters most for session mode, since a session makes dozens of calls per round, so meeting at least one throttle is close to certain, and without a retry a single blip discarded a task that had already spent minutes and dollars. A retried-away failure is not billed: usage is recorded from the decoded response, which the swallowed attempts never reach. Only Gemini and OpenAI are wrapped; the Anthropic provider uses the official SDK, which already retries.

Set `KIWI_OPENAI_BASE_URL` to point the OpenAI provider at a compatible endpoint (Azure, a gateway, a self-hosted server) instead of `api.openai.com`.

The **worker model is yours to choose, not the planner's**: `-model` (and the dashboard's model selector) is applied to every worker the plan produces, overriding anything the planning model suggested. The planner is never told which providers your org holds keys for, so we never ask it to pick one. A model id selects the provider, and a guessed one would route the work to a key you never connected. `-planner-model` selects the model that decomposes the task; both run on your own provider key.

### 3. Run the Data Plane daemon

```bash
./kiwidaemon -api-url https://api.runkiwi.dev \
    -key-path ~/.kiwi/daemon.key -cache-dir /tmp/kiwi-cache \
    -poll-interval 5s -max-cached-repos 20 -max-steps 6 -max-budget 0.50 \
    -session-budget 5.00 -join-token "$KIWI_JOIN_TOKEN"
```

On first boot the daemon generates its keypairs and registers with the Control Plane using a **single-use join token** (mint one with `POST /api/v1/daemon/join-token`, or from the dashboard's Fleets page). Once registered its persisted identity key is sufficient and the token can be omitted on restart. It then heartbeat-polls for work and runs each task through the Actor–Critic loop (`-max-steps` iterations / `-max-budget` USD per task cap the loop). Session-mode tasks are capped separately by `-session-budget` (or `KIWI_SESSION_BUDGET_USD`), default `5.00`, because the two loops have different economics, and a session costs several times what a file_loop task does, so `-max-budget` deliberately does not apply to it. The env fallback is what makes the setting reachable on the shared Free tier, where the provisioner launches per-org daemons with a fixed argv. The git cache keeps at most `-max-cached-repos` bare clones (default 20), evicting the least-frequently-used; `0` disables the bound. For the shared Free tier, pass `-sandbox-runtime runsc` (or `KIWI_SANDBOX_RUNTIME=runsc`) so the test command runs under gVisor; the wall-clock cap per task comes from the org's `TaskTimeoutSeconds` limit: **20 minutes on Free**, 30 on every other plan. Free is deliberately shorter because wall clock is what the tier meters: 20 minutes is 20 of the org's 500 agent-minutes, so the cap and the monthly allowance are one lever, not two. Within a session that budget is spread across rounds rather than spent on one. The per-round deadline derives from the session's own clock (a third of it, up to a 15-minute ceiling), because the value of several rounds is the Architect review *between* them, and a single round that consumes the whole cap produces no review anyone can act on.

### 4. Dashboard

```bash
KIWI_CORS_ALLOWED_ORIGINS=http://localhost:3000 ./kiwid -addr :8080 -dsn "..."
cd frontend && cp .env.local.example .env.local   # set NEXT_PUBLIC_KIWI_API_URL=http://localhost:8080
npm ci && npm run dev                               # http://localhost:3000
```

## SDKs

Minimal v1 SDKs for programmatic submission (CI/CD, Sentry auto-triage) live in `sdk/`, published as `@runkiwi/sdk` on npm and `kiwi-sdk` on PyPI. Each directory carries its own README, which is what the registry renders as the package page.

```js
// Node (sdk/node): zero dependencies, Node 18+
const { KiwiClient } = require('@runkiwi/sdk');
const client = new KiwiClient('http://localhost:8080', process.env.KIWI_TOKEN);
const { job_id } = await client.submitTask({
  task: 'Fix flaky test', file: 'pkg/foo/foo.go', testCmd: 'go test ./...',
});
const job = await client.getJob(job_id);
```

```python
# Python (sdk/python)
from kiwi import KiwiClient
client = KiwiClient("http://localhost:8080", token)
result = client.submit_task(task="Fix flaky test", file="pkg/foo/foo.go", test_cmd="go test ./...")
job = client.get_job(result["job_id"])
```

Both submit to `/api/v1/planner/plan`, the daemon-fed path `kiwi submit` uses, so a submission is planned into a DAG and leased by a daemon. Workers run asynchronously; poll `getJob` / `get_job` for the PR. Both constructors refuse to send a token over cleartext HTTP to a non-local host.

## Webhooks

The Control Plane exposes webhooks for third-party integrations:
- `POST /api/v1/webhooks/linear`: Issues labeled `kiwi` (or moved to **In Progress**) are converted into planner jobs. Requires `LINEAR_WEBHOOK_SECRET` to be set.
- `POST /api/v1/webhooks/github`: on a PR `closed` event where `merged` is true, Kiwi appends a `kiwi.ver/merge/v1` record to the org's chain, capturing **who approved the merge**, when, and the merge commit. A sealed record is never edited, so the approver arrives as a new link rather than a change to the execution record. Requires `GITHUB_WEBHOOK_SECRET`; without it the endpoint fails closed (503). Deliveries that are not a merged PR return 200 and do nothing, and a redelivery is a no-op. `GET /api/v1/jobs/{id}/record` continues to return the **execution** record, since the merge record is a separate link in the same chain.

## Free-tier deployment

The Free tier is **live in production**, split across two execution substrates because `kiwi-api` / `kiwi-orchestrator` run on **Cloud Run**, which cannot run the provisioner's `docker run` launches or a gVisor (`runsc`) sandbox:

1. **Control plane on Cloud Run**: `kiwi-api`, `kiwi-orchestrator`, `kiwi-frontend`, backed by Cloud SQL (private IP). Cloud Run leaves `KIWI_PROVISIONER` unset, so its orchestrator keeps only the singleton sweepers and never attempts a `docker run`.
2. **A Docker + gVisor GCE VM** ("free-fleet host", `kiwi-free-fleet`) with `runsc` registered as a Docker runtime, on the same VPC as Cloud SQL. It runs the control-plane binary with `KIWI_PROVISIONER=docker` (which starts the provisioner independently of `-role`, so the host needs no orchestrator sweepers), and `KIWI_PUBLIC_API_URL=https://api.runkiwi.dev`, supervised by the `kiwi-provisioner` systemd unit in [`deploy/free-fleet/`](deploy/free-fleet/). The provisioner cold-starts a per-org `kiwidaemon` container on submit; the launcher bind-mounts the host `docker.sock` so each daemon's test sandbox runs as a sibling container under `runsc`.

   `KIWI_DAEMON_IMAGE` is deliberately **left unset**. Setting it to a registry reference turns on `docker run --pull=always` for every launch, but Docker resolves registry credentials client-side, inside the provisioner container, which is cut off from the cloud metadata endpoint by `harden-egress.sh`, so every cold start would fail to pull. The launcher instead uses the local `kiwidaemon:latest` tag, refreshed on the *host* by a systemd timer, where the credentials live.
3. The **`kiwidaemon` image** in Artifact Registry, built with `docker build --target kiwidaemon` (the root `Dockerfile` ships both `kiwid` and `kiwidaemon` targets).

Schema changes (`queued_tasks.started_at`, `jobs.agent_minutes`, `org_limits.max_agent_minutes_per_month`, the `fleets.type` `self-managed`→`managed` rename, and the provisioner's partial unique index) apply via the standard `kiwid -role migrate` job. **Pro** (dedicated) stays on per-org VMs.

## Operational notes

- In `production` mode, `KIWI_ENCRYPTION_KEY`, `KIWI_SERVER_TOKEN`, and `KIWI_CORS_ALLOWED_ORIGINS` must be set explicitly. For managed, set `KIWI_KMS_KEY` to use Cloud KMS envelope encryption instead of a static key.
- **Execution-record signing.** `KIWI_VER_SIGNING_KEY` is a base64 Ed25519 seed (32 bytes) or private key (64 bytes); `KIWI_VER_SIGNING_KEY_ID` names it so records stay verifiable across a rotation. Generate one with `openssl rand -base64 32`. **The key must be stable and shared across replicas.** A per-process key would make every record it signed unverifiable after a restart. When unset, records are still assembled and stored but marked `"attestation": "unsigned"`, and `/.well-known/kiwi-signing-keys.json` returns an empty key set; nothing else is affected. `KIWI_EXECUTION_MODE` (`managed`|`byoc`) is recorded per job and defaults from `KIWI_PROVISIONER`, and decides how the record describes who operated the data plane.
- The `/api/v1/planner/plan` endpoint supports idempotent submissions via the `Idempotency-Key` header.
- **Job control.** `POST /api/v1/jobs/{id}/cancel`, `POST /api/v1/jobs/{id}/retry`, `DELETE /api/v1/jobs/{id}`. Cancelling a *running* task works by revoking its lease, since the Control Plane cannot dial a daemon: the daemon's next renewal returns 409 and it aborts. Retry requeues only failed and cancelled tasks. Delete removes the queue rows but keeps the job's execution record, because those are hash-chained per org and removing a link breaks verification for every record after it.
- **Fleet-host autoscaling** (`pkg/fleethost`, optional). The machine the free-tier provisioner runs on can scale to zero. Set `KIWI_FLEET_HOST_{PROJECT,ZONE,INSTANCE}` to enable it; leaving them unset disables it entirely, which is what BYOC and local dev want. The Control Plane starts the VM through the cloud API on submit and an idle sweeper stops it after the queue has been empty for `KIWI_FLEET_HOST_IDLE_TTL` (default 20m). Work distribution stays a pull model throughout, since host lifecycle is a separate channel from task handout.
- **Actor output ceiling.** `KIWI_COMPLETION_MAX_TOKENS` (default 16000) bounds a completion, because the right value is a property of the model rather than of Kiwi. The multi-file Actor returns whole file contents as JSON, so output tokens bind first; a provider that hits the ceiling reports `provider.ErrTruncated` rather than returning partial text as though it were whole. Adaptive thinking draws on the same budget.
- Database migrations apply automatically on boot; in a multi-replica deployment run `kiwid -role migrate` once before serving instead (`KIWI_SKIP_BOOT_MIGRATE=true` on serving roles).

---

## Contributing & context for AI

For build/test conventions, the PR checklist, and instructions for AI assistants, see [CLAUDE.md](CLAUDE.md).

Every PR modifying the codebase must keep this README current. If no update is needed, add the `skip-readme-check` label to the PR.

## License

Licensed under the [Apache License 2.0](LICENSE). Copyright © 2026 RunKiwi.
