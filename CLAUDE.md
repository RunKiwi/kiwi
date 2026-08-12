# Kiwi Development Guide (CLAUDE.md)

This file provides system context, architecture guidelines, and current status for AI assistants and contributors working on the **Kiwi** codebase.

---

## 1. Project State & Architecture

Kiwi is a **BYOC (Bring Your Own Cloud) agentic execution platform**: a **Control Plane** admits a task and hands it out via a Postgres lease queue; a **Data Plane** daemon leases a worker, runs an **Architect/Implementer session** (`pkg/session`, in the daemon process) — a task-long Architect that plans and reviews, driving an agentic Implementer with real tool calls — verifying each round with the test command in a sandbox, then opens a PR. Run **managed** (Kiwi operates the Data Plane) or **BYOC** (the Data Plane runs in the customer's cloud).

**Kiwi does what you were asked, not what makes a check go green.** The task description is the objective; the test command is a **guard** proving the change broke nothing. It is not the definition of done. An earlier version returned early when the suite already passed, which made every additive request ("add an example") a silent no-op — see `pkg/session/session.go` and its tests before changing anything about the success criterion.

**What's live:** the full path works end-to-end. A task posted to `/api/v1/planner/plan` is planned, leased by a per-org daemon, run through the loop with the test command in a **gVisor** sandbox, and returned as a PR. Three LLM providers are first-class (Anthropic, Gemini, OpenAI), routed by model id through `provider.ProviderOf`. The sandbox runs in **two phases** — a networked dependency install holding no credentials, then offline verification of model-generated code — so no repo is unverifiable merely because it has dependencies. Each run produces a signed, hash-chained **execution record** (`pkg/ver`). A self-serve **Free tier** runs every signup on a Kiwi-operated **shared fleet** — a per-org `kiwidaemon` process cold-started on submit by the **provisioner** (`ee/provisioner`), gVisor-sandboxed, agent-minute-metered, with rolling-window abuse auto-suspend.

**Active vs. dormant packages** — not everything in the tree is on the live path:
- **Active:** `ee/cmd/kiwid`, `cmd/kiwidaemon`, `ee/orchestrator`, `pkg/daemon`, `pkg/session`, `pkg/provider`, `pkg/ver`, `ee/planner`, `ee/provisioner`, `ee/entitlement`, `pkg/catalog`, `pkg/store`, `pkg/crypto`, `pkg/gitcache`, `pkg/sandbox`, `ee/auth`.
- **Built but not switched on in production** — the code is wired and tested; the deployment is not configured, so treat "it exists" and "it runs" as different claims: `ee/billing` (Stripe Checkout + webhook are implemented, but no `STRIPE_*` variables are set in prod, so Pro is a contact flow today), `ee/fleethost` (fleet auto-stop is a no-op unless `KIWI_FLEET_HOST_*` is set — it is not).
- **Dormant** — compiled and tested, but not on the active path; don't build on them without discussion: `pkg/tunnel`/`ee/tunnel`, `ee/audit`, `pkg/agentapi`, `pkg/checkpoint`.

**In progress:** a **Firecracker** managed-*dedicated* path (built, not deployed or hardware-validated), and finishing the **Pro** upgrade (see above — the blocker is configuration and a pricing decision, not code).

**The single-file loop is gone.** `pkg/loop` (the Actor–Critic loop over one planner-assigned file) and the `file_loop` execution mode were retired: session is the only loop. Decomposing a task into file-sized pieces existed to serve a single-turn Actor, and an Implementer that can `grep` needs no such guess — so the Control Plane stopped decomposing, which also removed a frontier-model call at submit time and a read of the org's decrypted key (a BYOC containment gap). `mode` is still accepted on the API, CLI and SDK and ignored, so nothing written against the two-mode API breaks. Do not reintroduce a "simple mode": the reason the old one produced poor work is structural, not tunable.

**Shipped since this section last said otherwise:** multi-tenant **egress isolation** on the free-fleet host is live — the cloud metadata endpoint is blocked in the `DOCKER-USER` chain (`deploy/free-fleet/harden-egress.sh`), verified from inside a running container. Note that chain applies only to *forwarded* traffic, so a `--network host` container bypasses it entirely; never run one on a fleet host.

### Key Architectural Constraints for New Code:
- **Language**: Go (`go.mod` targets 1.25).
- **Persistence**: **PostgreSQL** via GORM. Use strong consistency (transactional outbox) for state transitions. Numbered `migrations/*.up.sql` files are applied by `RunMigrations` (`ee/orchestrator/migrate.go`), tracked in `schema_migrations`, and run via `kiwid -role migrate` (prod serving roles set `KIWI_SKIP_BOOT_MIGRATE=true`). Note the schema has drifted from `migrations/0001` — some tables (e.g. `queued_tasks`, `credentials`) exist only via `AutoMigrate` in `ee/orchestrator/db.go`. Add data/DDL changes as a new numbered migration.
- **Queue**: **NATS JetStream** for durable queuing/event streaming; the BYOC daemon handoff uses the Postgres **lease queue** (`pkg/store/queue.go`) — tasks are leased, not popped, so a crashed daemon's work returns to the queue.
- **Multi-tenant**: Every task-scoped row carries `org_id`.
- **Security**: Secrets are never persisted in the sandbox. Customer credentials are sealed to the daemon's X25519 public key (`pkg/crypto`); the Architect and Implementer run in the daemon process, and only the commands they ask for run in the sandbox, so model-generated code never sees the model keys.
- **The two-phase sandbox invariant.** Phase A installs dependencies with the network **on** and a **nil environment** — no git token, no registry credential. Phase B runs the test command over model-generated code with the network **off** and credentials present. State it precisely: *model-generated code never has network access, and the phase that does never holds a secret.* Do not "simplify" this by giving Phase A credentials or Phase B a network; each collapses the property the split exists to create.
- **Zero-knowledge is a BYOC-only claim, and it does not cover repository access.** In managed mode Kiwi operates the machine holding the private key and *can* decrypt. Do not write docs or code comments claiming zero-knowledge for managed mode. Since the GitHub App landed there is a second limit that applies in **both** modes: the Control Plane holds the App's private key and mints installation tokens (`ee/githubapp`), so for any org that connected GitHub, Kiwi can mint a token for the repositories that org granted. The honest claim is scoped and revocable access, not no access: the token covers only the repositories the customer ticked, expires within the hour, and the customer can revoke the installation themselves. It is strictly better than the PAT it replaces, which was org-wide and immortal. It is *not* zero-knowledge, and no doc may say otherwise.
- **Licence boundary.** Everything outside `ee/` is Apache-2.0; `ee/` is BSL 1.1. `ee/` may import Apache-2.0 packages; **Apache-2.0 code must never import `ee/`** — that would pull BSL terms into code documented as Apache-2.0, and the people it lands on are BYOC customers whose legal review reads exactly this line. `pkg/licensing_boundary_test.go` enforces it and names the offending file. If an Apache-2.0 package needs Control-Plane behaviour, define an interface there and let `ee/` supply the implementation. `cmd/kiwidaemon` depending on nothing in `ee/` is the property that matters most.
- **Terminology**: The supported LLM providers are Anthropic, Gemini, OpenAI, or compatible endpoints. The canonical identifier for the third is `openai` (credential `OPENAI_API_KEY`, integration key `openai`) — the earlier `codex` naming is retired and must not be reintroduced.
- **Provider routing**: `provider.ProviderOf(model)` is the single mapping from a model id to its provider, and `provider.CredentialNameFor` from a provider to its key. Adding a provider means adding it there, to `PricingMap`, to `daemon.isLLMKey`, to `integrationSpec`, and to the frontend's `providerOf` — never re-deriving the rule in a new place.

---

## 2. Mandatory Pre-Commit Checks
CI enforces these on every PR. Run them locally **before every commit**:
```bash
gofmt -l cmd/ pkg/ ee/             # MUST print nothing. Fix with: gofmt -w cmd/ pkg/ ee/
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
go build -ldflags="-linkmode=external" -o kiwid ee/cmd/kiwid/main.go && codesign -s - -f ./kiwid

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
kiwi/                     # Apache-2.0 unless noted
├── cmd/
│   ├── kiwi/             # CLI client (login, submit, claude)
│   ├── kiwidaemon/       # BYOC Data Plane daemon (out-bound polling)
│   └── kiwi-agent/       # In-sandbox agent entrypoint
├── pkg/
│   ├── daemon/           # BYOC daemon: heartbeat client, poll loop, runtime/ecosystem inference, two-phase sandbox
│   ├── session/          # The Architect/Implementer loop itself (runs in the daemon process, never in the sandbox)
│   ├── ver/              # Signed, hash-chained execution record (kiwi.ver/v1)
│   ├── crypto/           # X25519 sealing + Ed25519 signing
│   ├── gitcache/         # Bare clone + git worktree provisioning
│   ├── catalog/          # Model discovery: per-provider listers + catalog refresh
│   ├── store/            # Postgres models, lease queue, sealed credentials, model catalog
│   ├── queue/            # NATS JetStream relay + consumer
│   ├── agent/            # In-sandbox master/worker runtime
│   ├── sandbox/          # Execution isolator (Docker + gVisor); two-phase: networked install, offline verify
│   ├── infra/            # Docker driver
│   ├── provider/         # Anthropic / Gemini / OpenAI / OpenRouter clients + the registry and routing rule
│   ├── manifest/         # Manifest generation
│   ├── client/           # Go client for the Control Plane API
│   ├── tunnel/           # [paused] Reverse credential proxy — CLIENT half only
│   ├── agentapi/         # [paused] Master-worker API
│   ├── checkpoint/       # [paused] Event log / snapshotting
│   └── licensing_boundary_test.go   # enforces the split below
├── ee/                   # BUSINESS SOURCE LICENSE 1.1 — see ee/LICENSE
│   ├── cmd/kiwid/        # Control Plane daemon (API + orchestrator roles)
│   ├── cmd/kms-migrate/  # Control Plane KMS migration tool
│   ├── orchestrator/     # Core engine, HTTP server, webhooks, daemon seam
│   ├── planner/          # Task -> worker DAG decomposition, admission
│   ├── auth/             # Orgs, API keys, limits
│   ├── entitlement/      # Per-tier token allowances on Kiwi-owned keys
│   ├── billing/          # Stripe Checkout + webhook — implemented, not configured in prod (see §1)
│   ├── provisioner/      # Free tier: launches per-org daemon containers (docker + runsc)
│   ├── fleethost/        # Fleet auto-stop controller (inert unless KIWI_FLEET_HOST_* is set)
│   ├── dashboard/        # Server-rendered dashboard
│   ├── audit/            # [paused] Audit log
│   └── tunnel/           # [paused] Reverse credential proxy — SERVER half
├── frontend/             # Next.js dashboard
├── sdk/                  # Node + Python SDKs
├── migrations/           # Postgres schema (see drift note in §1)
├── docs/                 # Public assets (logo)
└── demo_project/         # A buggy Go project used for verification testing
```
