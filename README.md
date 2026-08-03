<p align="center">
  <img src="docs/assets/kiwi-logo.svg" width="88" alt="Kiwi logo" />
</p>

# Kiwi

**Kiwi runs coding agents inside infrastructure you control, and shows its work.**

A SaaS **Control Plane** decomposes a task into a DAG of workers. A **Data Plane** runs each worker in an isolated sandbox through an **Actor–Critic loop** — editing files and re-running your test command until it passes — then opens a PR. Run it **managed** (Kiwi operates the execution) or **BYOC** (the Data Plane runs in your own cloud, where code and credentials never leave your VPC).

Two properties hold on every task, in both modes:

- **The execution is contained.** Model-generated code only ever runs as your test command, inside a sandbox with default-deny networking, and never sees an API key — the Actor and Critic run in the daemon process, not in the sandbox.
- **The execution is on the record.** Every proposed edit, every Critic verdict and its reasons, and every test run is persisted per phase with its model, token counts, cost and duration — so a merged diff can be traced back to what produced it and what proved it.

## Try it

**[Sign up or log in at app.runkiwi.dev →](https://app.runkiwi.dev)** — the fastest way to run Kiwi, no setup required. Sign in with GitHub or Google; every account starts on the **Free tier** (a Kiwi-operated shared fleet). Connect a repo, add your own model key, and submit a task — the swarm plans it, runs it, and opens a PR.

Prefer to run it yourself? See the self-host [Quickstart](#quickstart) below.

> [!NOTE]
> **Live, still maturing.** A task flows **end-to-end** — submit one and get a real PR back (`make local`, below). The self-serve **Free tier is deployed to production**: a signup runs tasks on a Kiwi-operated **shared fleet** without contacting us (per-org daemon processes, gVisor sandbox, agent-minute metering), served from a Cloud Run control plane plus a Docker + gVisor free-fleet host (see [Deployment](#free-tier-deployment)). **Pro** upgrade billing is wired via Stripe Checkout (test mode). Still in progress: hardened multi-tenant **egress** isolation, and the Firecracker managed-*dedicated* path.

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
- **Isolation**: the LLM Actor/Critic run **in the daemon process**; only the test command runs in the sandbox, so model-generated code executes with **default-deny networking** and never sees the LLM key. The sandbox driver is pluggable (`pkg/sandbox`) — Docker for dev/BYOC, **gVisor (`runsc`) for the shared Free tier** (set per-daemon via `KIWI_SANDBOX_RUNTIME=runsc`), and a Firecracker microVM driver for hardened managed execution (`KIWI_SANDBOX=firecracker`). On the shared free-fleet host, per-org daemon containers are additionally firewalled off the cloud **metadata endpoint**, so a container can't lift the host's service-account token ([`deploy/free-fleet/`](deploy/free-fleet/)).
- **Tiers**: a **Free** tier runs every signup on a Kiwi-operated **shared fleet** — one lightweight daemon *process* per org (its own keypair, so the credential-sealing model is unchanged), packed onto shared hosts and scaled to zero when idle. Usage is bounded by per-org limits: one concurrent job, a per-task wall-clock cap, and a monthly **agent-minute** ceiling; a cryptomining heuristic auto-suspends abusive orgs. A per-org daemon is cold-started on submit by the **provisioner** (`pkg/provisioner`), which consumes `ProvisioningRequest`s and launches per-org `kiwidaemon` containers. **Pro** graduates to a dedicated fleet (managed-dedicated or BYOC).
- **Credentials**: the daemon generates an X25519 keypair (credential sealing) and an Ed25519 keypair (heartbeat signing) on boot. Customer credentials are stored by the SaaS **sealed to the daemon's X25519 public key**, and at rest are encrypted via the configured key manager (a static key for dev/BYOC, **Cloud KMS envelope encryption** for managed — `pkg/crypto`). `GIT_TOKEN` authenticates both **reading** the repository (the bare clone and every fetch) and pushing the result, so **private repositories work**. On the read side it is passed as a URL-scoped `http.<host>.extraHeader` rather than embedded in the remote URL, because `git clone` persists its URL argument into the bare repo's config — a `-c` value lives only for that one process, and scoping it to the remote's host keeps it off any redirect to a third party. Cloning happens in the daemon, never in a sandbox.
- **Shared context** (opt-in): when submitting a task, the planner can draw on your org's **prior jobs** so related work informs how the new task is decomposed. Choose **Auto** (semantic nearest-neighbour over past jobs via pgvector embeddings) or **Manual** (pick specific jobs). It is **off by default**, strictly **org-scoped** (you only ever reference your own jobs), and injected at **plan time only**; Auto may use extra tokens.
- **A run can be watched while it runs**: the daemon executes the Actor–Critic loop in its own process, so it is the only component that sees a run happen — and it used to keep everything until the end, which made a task that was stuck indistinguishable from one working hard. It now flushes telemetry every few seconds (`POST /api/v1/daemon/progress`), and `GET /api/v1/jobs/{id}/progress` serves the phases completed so far, the command running right now, and the tail of its output. The dashboard renders it with the same timeline component a finished job uses, so nothing shifts when the run ends. Three properties make it safe to ignore: it travels on its own call rather than the lease renewal (a failed progress post must never cost a daemon the task it is running), it is fenced by the lease id like every other task write, and the final result report replaces what was streamed rather than appending to it — otherwise every phase, and the cost attributed to the run, would be counted twice. When the daemon stops reporting, the UI says so; a timestamp that stops advancing is how a hung run tells itself apart from a slow one.

- **Queue diagnostics**: a task sits in `QUEUED` until a daemon leases it, and the reasons it might not be leased are otherwise invisible — no runner is registered for its fleet, a free-tier cold-start is still in flight or failed outright, the org is at its concurrency or agent-minute cap, or the task's DAG dependencies are unfinished. `GET /api/v1/jobs/{id}` therefore returns a `blocked_reason` code and a human-readable `blocked_detail` for every queued task, alongside `queued_at` / `started_at` / `attempts` / `leased_by` (`pkg/store/queue_diagnose.go`). The check mirrors the lease rules exactly, including fleet routing, and is a pure read that never affects scheduling. The dashboard renders it in place of a bare spinner, distinguishing "starting your runner" from "no runner connected". A failed provisioning attempt records **why** on the request row, so the reason reaches the org whose runner never started rather than dying in a log on the provisioning host.
- **Telemetry actually persists**: the per-phase evidence an execution record is assembled from could never be written. `task_events.task_id` referenced `task_states` — where tasks lived before the daemon seam moved them to `queued_tasks` — so every insert violated the foreign key. The write is best-effort by design, because telemetry must never fail a result report, so the error was logged and swallowed: records were assembled with empty worker steps and nothing surfaced the outage. Migration `0018` drops the stale constraint (see the note there on why it is not simply re-pointed).
- **The run explains itself**: the job drawer renders the Actor–Critic loop phase by phase — what the Actor proposed, what the Critic ruled, and *why*, quoted in the Critic's own words. Previously a failed job's entire account of itself was one line of result text, so a ten-minute run rejected three times for the same reason reported only "reached max steps without passing", and the explanation lived in a daemon log on a machine the user cannot reach. The data was already in the execution record; nothing rendered it. Raw test output is still never carried — only a digest — because it can contain secrets.
- **A wrong filename is repaired, not retried**: the planner names files without seeing the repository, and the Actor may change a file's contents but never its name — so a new file planned as `examples/advanced.rs` in a Go project is a position the loop cannot win, however good the code. A to-be-created file whose extension unambiguously names another language is corrected against the repository's own ecosystem (`.rs` → `.go`); `.md`, `.yaml` and anything ambiguous are left alone. And when the Critic rejects three attempts in a row, the loop stops and reports its last reason rather than spending the remaining budget being told the same thing.
- **The tests are a guard, not the goal**: Kiwi does what you ask; the test command exists to prove the change did not break anything. A green suite is therefore not a reason to skip the work — that assumption made every *additive* task a silent no-op, since adding an example does not make `go build` start failing, so the Actor was never invoked and the task reported success having changed nothing. One rule now covers both directions: the test state must not get worse (`red → green` fixes it, `green → green` preserves it), and the run must produce an actual diff. Anti-gaming still applies exactly where it matters — while the suite is failing, a failing test defines the job and the agent may not edit it, because it could pass the gate by weakening the assertion. Once the suite is green that test file defines nothing, so "add tests for the parser" is an ordinary task rather than something to refuse.
- **A run that changes nothing is not a success**: an unchanged worktree is reported as a failure with the reason, not as a green tick with no pull request.
- **When a repo cannot be verified offline, it says so**: some projects need the network to *build*, not merely to install — a Next.js app importing `next/font/google` downloads the font on every build, and no amount of pre-installing helps, because the fetch is part of the build and the build is what must run without network. That is unfixable by an agent editing code, so the first verification run is classified: a network failure there, before any edit has been applied, describes the repository rather than the agent, and the task ends immediately with the reason ("this project's build downloads Google Fonts… use a test command that runs offline, or self-host the font") instead of spending six Actor steps and the user's whole budget discovering it.
- **Two-phase sandbox**: every ecosystem needs a network fetch before its tests can run (`npm ci`, `pip install`, `go mod download`, `cargo fetch`), and the verification sandbox is default-deny by design, so the two are separated. **Phase A** installs the repository's own declared dependencies — chosen by lockfile, so `yarn.lock` and `package-lock.json` are never confused — with network enabled and **no credentials at all**: not the git token, not a registry secret, not the LLM keys already withheld from every sandbox. **Phase B** runs the test command with `--network none`, and is the only phase that executes model-generated code. Stated precisely, the guarantee is stronger than "the sandbox has no network": *model-generated code never has network access, and the phase that does never holds a secret*, so a malicious postinstall hook can reach the network but has nothing to send. Package caches are redirected (`GOMODCACHE`, `CARGO_HOME`, `BUNDLE_PATH`, `PIP_TARGET`…) into a directory mounted into both phases and kept **outside the worktree**, since delivery runs `git add -A` — without that, a toolchain that downloads into its own home rather than the project directory loses everything when the install container exits. The trade is explicit: dependencies behind a private registry cannot be installed, because handing that credential to a networked container running third-party install hooks is the exposure this split exists to avoid.
- **A dependency the agent adds gets installed**: Phase A runs before the loop, so a package the Actor adds afterwards was never fetched and offline verification would fail on a missing module however good the edit. The install phase therefore **runs again** whenever a dependency manifest changes, and both halves of the guarantee above hold unchanged — the re-install still has network and no credentials, verification still runs offline. Two details make it work: the frozen command becomes the resolving one (`npm ci` *fails* when `package.json` and the lockfile disagree, which is exactly what adding a dependency creates; `go mod tidy` rather than `go mod download`, since an edited `go.mod` needs its `go.sum` hashes written), and the change is detected by hashing manifest **contents**, because the Actor rewrites whole files and an identical rewrite must not cost a five-minute re-install. A re-install that fails is handed back as the test output rather than ending the task, so `404 Not Found — GET …/nonexistent-pkg` reaches the Actor, which can correct the package name. This previously refused any task touching a manifest, which ruled out a large share of ordinary requests ("add a cookie banner, use a library if there is one") before the model was called once.
- **The sandbox works out its own runtime**: you submit a prompt, not a build environment. The daemon picks the container image from what the repository already declares — `devcontainer.json` if present, then the test command's own executable (it names the binary that must exist, which is what settles a polyglot repo), then marker files, with versions read from `go.mod`, `.nvmrc`, `engines.node` or `.python-version`. There is no image flag, and no question to ask the user. When the guess is still wrong the sandbox says so in terms that are machine-readable — `sh: npm: not found`, `go.mod requires go >= 1.25.0` — so the daemon distinguishes a broken environment from a failing test, repairs the image and re-runs **once, before the Actor is asked for anything**. Only the first failure is inspected, so a genuinely failing test costs nothing extra.
- **The planner proposes, the repo decides**: the planner runs on the Control Plane, which never clones a repository — it is given the repo URL, not its contents — so any file path or test command it emits is a guess from the model's priors. Those are treated as *hints* and resolved on the daemon, where the checkout actually exists: a target is matched against the real `git ls-files` tree (exact path, then file name, ambiguity left unresolved), an unmatched hint falls through to model-driven discovery over that tree, and only then to creating a new file. Test commands work the same way — the planner no longer emits one, so `inferTestCmd` reads the repo's own marker files instead. Previously a hint suppressed both, so the component that cannot see the repo overrode the one that can, and `components/Footer.tsx` against a repo that keeps it at `src/components/Footer.tsx` produced a duplicate rather than an edit.
- **Creating files, not just editing them**: a target that does not exist yet is a file to *create*. "Add a cookie consent popup" plans a new component, and the loop treats a missing target as empty starting content rather than an error, creating any missing parent directories on write. This applies to both the single-file and multi-file Actor paths — in the latter a missing target is offered to the model marked as new and stays in the write allowlist, so a proposed new file is not silently discarded. Only a genuinely unreadable path (a permission error, a directory in the way) still stops the loop.
- **Actor output limits**: the multi-file Actor returns whole file contents as JSON, which makes *output* tokens the binding constraint — with several candidate files a reply is cut off mid-JSON, and the parser's "unexpected end of JSON input" describes the symptom rather than the cause. Three things address it: the Actor is instructed to return **only the files it modifies**, both providers now surface a hit output ceiling as `provider.ErrTruncated` instead of returning the partial text as if it were whole, and the ceiling is `KIWI_COMPLETION_MAX_TOKENS` (default 16000) because the right value is a property of the model, not of Kiwi. Note adaptive thinking draws on the same budget as the visible answer.
- **Job control**: a job can be stopped, retried, or deleted — `POST /api/v1/jobs/{id}/cancel`, `POST /api/v1/jobs/{id}/retry`, `DELETE /api/v1/jobs/{id}`. A stop moves every non-terminal task to `CANCELLED`, a terminal state deliberately distinct from `FAILED` (a job you called off did not fail). Because the Control Plane cannot dial a daemon, stopping a *running* task works by revoking its lease: the daemon's next renewal returns 409, and it aborts the run rather than burning metered minutes on work whose result would be rejected anyway by the fencing token. Retry requeues only failed and cancelled tasks and resets their attempt count; succeeded tasks are never re-run. Delete cancels first, then removes the queue rows — but **keeps the job's execution record**, since those are hash-chained per org and removing a link would break verifiability for every record after it.
- **Fleet-host autoscaling** (`pkg/fleethost`, optional): the machine the free-tier provisioner runs on can scale to zero. Work distribution stays a pull model — nothing ever dials the daemon — because host lifecycle is a *different channel*: the Control Plane starts the VM through the cloud API on submit, and the daemon resumes polling on its own once it boots. An idle sweeper in the orchestrator role stops the host after the queue has been continuously empty for `KIWI_FLEET_HOST_IDLE_TTL` (default 20m), re-checking immediately before it acts and treating any unreadable queue as activity, so a machine is never stopped out from under in-flight work. Enabled by setting `KIWI_FLEET_HOST_{PROJECT,ZONE,INSTANCE}`; unset (BYOC, local dev) disables it entirely. The cold start it introduces is visible as the `provisioning` blocked reason, not an unexplained wait.
- **Execution record** (`pkg/ver`): when every task in a job reaches a terminal state, the Control Plane assembles a per-job provenance record — the plan's content hash and planner model, each worker's Actor/Critic steps with the Critic's verdicts, the test command and its outcome, and the PR. The daemon signs the execution half with the **Ed25519 key it generated on boot** (so in BYOC that half is attested by a key the Control Plane never holds), and the Control Plane counter-signs the whole. Records are chained per org (`prev_record_hash`) and appended under one transaction, so a gap or reorder is detectable. Fetch one with `GET /api/v1/jobs/{id}/record`; the signing public key is published at `/.well-known/kiwi-signing-keys.json` so a record can be verified **offline**. Raw test output and diffs are **never** stored in the record — only SHA-256 digests, plus a bounded quote of the Critic's own reasons.
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
| Queue diagnostics — a queued task reports *why* it hasn't started | ✅ |
| Job control — stop / retry / delete, with a real abort on the daemon | ✅ |
| Fleet-host autoscaling — scale the free-fleet machine to zero when idle | ✅ (opt-in) |
| Integration layer — `kiwi` CLI, Node/Python SDKs, Linear webhook | ✅ |
| Shared context — plan with prior-job learnings (Auto pgvector search / Manual select), org-scoped, opt-in | ✅ |
| Execution record — per-job provenance, daemon-attested + CP-signed, hash-chained per org (`pkg/ver`) | ✅ Records assemble and sign; set `KIWI_VER_SIGNING_KEY` or they persist `unsigned` |
| Plan validation — reject cyclic/dangling dependencies, duplicate IDs, and undeclared file conflicts at submit time | ✅ |
| Merge provenance — GitHub PR-merge webhook appends a signed `kiwi.ver/merge/v1` link capturing the approver | ✅ Set `GITHUB_WEBHOOK_SECRET` |
| **Free tier — live in production** (`app.runkiwi.dev`): per-org daemon provisioner, gVisor sandbox, agent-minute metering & abuse suspend | ✅ Deployed — Cloud Run control plane + Docker/gVisor free-fleet host (see [Deployment](#free-tier-deployment)) |
| Control plane on GCP — Cloud Run (`kiwi-api`/`kiwi-orchestrator`/`kiwi-frontend`), Cloud SQL, KMS, OAuth sign-in | ✅ Deployed |
| Self-serve signup & tenancy (GitHub/Google OAuth, per-org isolation) | ✅ Signup path live |
| Billing — Stripe Checkout for the **Pro** upgrade + signed webhook (plan/limits) | ✅ Wired (test mode); set `STRIPE_*` env to enable, else the free path is unaffected |
| Managed-**dedicated** (Pro) — per-org VM Terraform (`deploy/gcp/`), KMS envelope crypto, Firecracker driver | 🚧 Built; not yet deployed or hardware-validated |
| Egress isolation — sandbox `--network none` (enforced + tested) + host metadata-endpoint hardening (`deploy/free-fleet/`) | ✅ Shipped; apply on the fleet host |
| Session loop — a task-long Architect (plan + review) driving an agentic Implementer with real tool calls, in reviewed rounds | 🚧 Building, phase by phase — [docs/rfc-session-loop.md](docs/rfc-session-loop.md); `pkg/loop` stays the default path |
| ├ Tool-calling seam (`provider.ToolRunner`) + persistent sandbox (`sandbox.Session`) + cache-aware pricing | ✅ Phase 0 |
| ├ `pkg/session` — Architect plans/reviews, Implementer works with tools; opt-in via `spec.mode: session` | ✅ Phase 1 — the sandbox gets **no credentials** in this mode (`KIWI_SESSION_ALLOW_TEST_CREDS` opts back in) |
| ├ Crash recovery — round-level checkpoints (`agent_sessions`, migration 0021); a re-leased task resumes at its last finished round | ✅ Phase 2 |
| ├ Cost — prompt caching on by default, cache-priced budgets, mid-round transcript compaction | ✅ Phase 3 |
| ├ Planner collapse — one worker per session job, **no LLM call and no credential decryption on the Control Plane** (`KIWI_SESSION_MODE=off` disables) | ✅ Phase 4 |
| └ Provider parity — tool-calling on Anthropic, Gemini and OpenAI, so session mode is not one vendor's feature | ✅ Phase 5 — Gemini additionally echoes the `thoughtSignature` it requires back on replay; without it the second tool turn of every conversation is rejected |
| └ Reachable from the clients — `-mode session` on the CLI, `mode` in the SDK/API, an **Execution loop** control in the dashboard | ✅ Proven end to end in production: Architect plans, Implementer works with tools, reviewer approves, PR opened. `file_loop` stays the default everywhere and the key is omitted entirely unless session is chosen |

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

The same rule decides which key the planner uses, how a call is priced, and which provider the signed execution record names — it is one function (`provider.ProviderOf`) rather than a rule repeated per component. If a task fails because a key is missing, invalid, or out of credits, the reason is surfaced on the job.

**Transient provider failures are retried** (`pkg/provider/retry.go`): `429` and `5xx` are retried with exponential backoff and jitter, honouring the provider's own `Retry-After` when it sends one, and never sleeping past the caller's deadline. This matters most for session mode — a session makes dozens of calls per round, so meeting at least one throttle is close to certain, and without a retry a single blip discarded a task that had already spent minutes and dollars. A retried-away failure is not billed: usage is recorded from the decoded response, which the swallowed attempts never reach. Only Gemini and OpenAI are wrapped; the Anthropic provider uses the official SDK, which already retries.

Set `KIWI_OPENAI_BASE_URL` to point the OpenAI provider at a compatible endpoint (Azure, a gateway, a self-hosted server) instead of `api.openai.com`.

The **worker model is yours to choose, not the planner's**: `-model` (and the dashboard's model selector) is applied to every worker the plan produces, overriding anything the planning model suggested. The planner is never told which providers your org holds keys for, so it is not asked to pick one — a model id selects the provider, and a guessed one would route the work to a key you never connected. `-planner-model` selects the model that decomposes the task; both run on your own provider key.

### 3. Run the Data Plane daemon

```bash
./kiwidaemon -api-url https://api.runkiwi.dev \
    -key-path ~/.kiwi/daemon.key -cache-dir /tmp/kiwi-cache \
    -poll-interval 5s -max-cached-repos 20 -max-steps 6 -max-budget 0.50 \
    -session-budget 5.00 -join-token "$KIWI_JOIN_TOKEN"
```

On first boot the daemon generates its keypairs and registers with the Control Plane using a **single-use join token** (mint one with `POST /api/v1/daemon/join-token`, or from the dashboard's Fleets page). Once registered its persisted identity key is sufficient and the token can be omitted on restart. It then heartbeat-polls for work and runs each task through the Actor–Critic loop (`-max-steps` iterations / `-max-budget` USD per task cap the loop). Session-mode tasks are capped separately by `-session-budget` (or `KIWI_SESSION_BUDGET_USD`), default `5.00` — the two loops have different economics, and a session costs several times what a file_loop task does, so `-max-budget` deliberately does not apply to it. The env fallback is what makes the setting reachable on the shared Free tier, where the provisioner launches per-org daemons with a fixed argv. The git cache keeps at most `-max-cached-repos` bare clones (default 20), evicting the least-frequently-used; `0` disables the bound. For the shared Free tier, pass `-sandbox-runtime runsc` (or `KIWI_SANDBOX_RUNTIME=runsc`) so the test command runs under gVisor; the wall-clock cap per task comes from the org's `TaskTimeoutSeconds` limit.

### 4. Dashboard

```bash
KIWI_CORS_ALLOWED_ORIGINS=http://localhost:3000 ./kiwid -addr :8080 -dsn "..."
cd frontend && cp .env.local.example .env.local   # set NEXT_PUBLIC_KIWI_API_URL=http://localhost:8080
npm ci && npm run dev                               # http://localhost:3000
```

## SDKs

Minimal v1 SDKs for programmatic submission (CI/CD, Sentry auto-triage) live in `sdk/`, published as `@runkiwi/sdk` on npm and `kiwi-sdk` on PyPI. Each directory carries its own README, which is what the registry renders as the package page.

```js
// Node (sdk/node) — zero dependencies, Node 18+
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

Both submit to `/api/v1/planner/plan` — the daemon-fed path `kiwi submit` uses — so a submission is planned into a DAG and leased by a daemon. Workers run asynchronously; poll `getJob` / `get_job` for the PR. Both constructors refuse to send a token over cleartext HTTP to a non-local host.

## Webhooks

The Control Plane exposes webhooks for third-party integrations:
- `POST /api/v1/webhooks/linear`: Issues labeled `kiwi` (or moved to **In Progress**) are converted into planner jobs. Requires `LINEAR_WEBHOOK_SECRET` to be set.
- `POST /api/v1/webhooks/github`: on a PR `closed` event where `merged` is true, Kiwi appends a `kiwi.ver/merge/v1` record to the org's chain, capturing **who approved the merge**, when, and the merge commit. A sealed record is never edited, so the approver arrives as a new link rather than a change to the execution record. Requires `GITHUB_WEBHOOK_SECRET`; without it the endpoint fails closed (503). Deliveries that are not a merged PR return 200 and do nothing, and a redelivery is a no-op. `GET /api/v1/jobs/{id}/record` continues to return the **execution** record — the merge record is a separate link in the same chain.

## Free-tier deployment

The Free tier is **live in production**, split across two execution substrates because `kiwi-api` / `kiwi-orchestrator` run on **Cloud Run**, which cannot run the provisioner's `docker run` launches or a gVisor (`runsc`) sandbox:

1. **Control plane on Cloud Run** — `kiwi-api`, `kiwi-orchestrator`, `kiwi-frontend`, backed by Cloud SQL (private IP). Cloud Run leaves `KIWI_PROVISIONER` unset, so its orchestrator keeps only the singleton sweepers and never attempts a `docker run`.
2. **A Docker + gVisor GCE VM** ("free-fleet host", `kiwi-free-fleet`) with `runsc` registered as a Docker runtime, on the same VPC as Cloud SQL. It runs the control-plane binary with `KIWI_PROVISIONER=docker` (which starts the provisioner independently of `-role`, so the host needs no orchestrator sweepers), and `KIWI_PUBLIC_API_URL=https://api.runkiwi.dev`, supervised by the `kiwi-provisioner` systemd unit in [`deploy/free-fleet/`](deploy/free-fleet/). The provisioner cold-starts a per-org `kiwidaemon` container on submit; the launcher bind-mounts the host `docker.sock` so each daemon's test sandbox runs as a sibling container under `runsc`.

   `KIWI_DAEMON_IMAGE` is deliberately **left unset**. Setting it to a registry reference turns on `docker run --pull=always` for every launch, but Docker resolves registry credentials client-side — inside the provisioner container, which is cut off from the cloud metadata endpoint by `harden-egress.sh` — so every cold start would fail to pull. The launcher instead uses the local `kiwidaemon:latest` tag, refreshed on the *host* by a systemd timer, where the credentials live.
3. The **`kiwidaemon` image** in Artifact Registry — `docker build --target kiwidaemon` (the root `Dockerfile` ships both `kiwid` and `kiwidaemon` targets).

Schema changes (`queued_tasks.started_at`, `jobs.agent_minutes`, `org_limits.max_agent_minutes_per_month`, the `fleets.type` `self-managed`→`managed` rename, and the provisioner's partial unique index) apply via the standard `kiwid -role migrate` job. **Pro** (dedicated) stays on per-org VMs.

## Operational notes

- In `production` mode, `KIWI_ENCRYPTION_KEY`, `KIWI_SERVER_TOKEN`, and `KIWI_CORS_ALLOWED_ORIGINS` must be set explicitly. For managed, set `KIWI_KMS_KEY` to use Cloud KMS envelope encryption instead of a static key.
- **Execution-record signing.** `KIWI_VER_SIGNING_KEY` is a base64 Ed25519 seed (32 bytes) or private key (64 bytes); `KIWI_VER_SIGNING_KEY_ID` names it so records stay verifiable across a rotation. Generate one with `openssl rand -base64 32`. **The key must be stable and shared across replicas** — a per-process key would make every record it signed unverifiable after a restart. When unset, records are still assembled and stored but marked `"attestation": "unsigned"`, and `/.well-known/kiwi-signing-keys.json` returns an empty key set; nothing else is affected. `KIWI_EXECUTION_MODE` (`managed`|`byoc`) is recorded per job — it defaults from `KIWI_PROVISIONER` and decides how the record describes who operated the data plane.
- The `/api/v1/planner/plan` endpoint supports idempotent submissions via the `Idempotency-Key` header.
- Database migrations apply automatically on boot; in a multi-replica deployment run `kiwid -role migrate` once before serving instead (`KIWI_SKIP_BOOT_MIGRATE=true` on serving roles).

---

## Contributing & context for AI

For build/test conventions, the PR checklist, and instructions for AI assistants, see [CLAUDE.md](CLAUDE.md).

Every PR modifying the codebase must keep this README current. If no update is needed, add the `skip-readme-check` label to the PR.

## License

Licensed under the [Apache License 2.0](LICENSE). Copyright © 2026 RunKiwi.
