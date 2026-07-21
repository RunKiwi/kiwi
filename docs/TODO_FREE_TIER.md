# Free Tier — Implementation TODO

**Companion to:** [RFC: Free Tier via Per-Org Daemon Processes on Shared Hosts](rfcs/2026-07-21-free-tier-shared-fleet-rfc.md). Read the RFC first — it explains *why* free = a per-org daemon **process** (not a shared daemon, not a per-org VM). This file is the *how*, decomposed so each phase can be picked up independently.

## How to work this TODO (read before starting any phase)

- **Each phase is self-contained:** it states what already exists, what to build, which files, and how it's verified. Do the phases in order — later phases assume earlier ones landed.
- **The goal of the whole effort:** a brand-new signup (an `Organization{Plan:"free"}`) can `kiwi submit` a task and get a PR back, running on a Kiwi-operated shared host pool, with each org isolated to its own daemon process + a gVisor-sandboxed test command.
- **Mandatory pre-commit checks (CLAUDE.md §2) — run before every commit, treat any failure as a blocker:**
  ```bash
  gofmt -l cmd/ pkg/                 # MUST print nothing (fix: gofmt -w cmd/ pkg/)
  CGO_ENABLED=0 go vet ./...         # MUST be clean
  CGO_ENABLED=0 go test ./pkg/...    # MUST pass
  CGO_ENABLED=0 go build ./...       # MUST build
  ```
- **Tests-first (CLAUDE.md §3):** every phase ships tests. Use stubs for infra/providers; do not require real cloud in CI.
- **Terminology (CLAUDE.md §1):** never write the literal three-letter Open+AI brand string in code/docs — use `codex`.
- **README:** if a phase changes user-facing behavior, update `README.md` or label the PR `skip-readme-check`.
- **Do not invent parallel systems.** Reuse `store.OrgLimits`, `SealCredentialsForDaemon`, `LeaseNextTask`, `DaemonJoinToken`, `ProvisioningRequest`. If you find yourself adding a *cross-org* lease or a *shared* daemon, stop — that is explicitly rejected (RFC §2.1, §9).
- **A reviewer will verify each phase** against its "Verification" section before the next begins.

## Ground truth: what already exists (verified 2026-07-21)

The daemon seam is **closed** — these are built, tested, and wired:

| Capability | Symbol / location |
| :--- | :--- |
| Org has a plan, defaults free | `Organization.Plan` — `pkg/store/models.go:14`, `pkg/auth/models.go:18` |
| Activation enqueues provisioning | `ActivateOrg` / `SuspendOrg` — `pkg/auth/activation.go:12,46` (writes `ProvisioningRequest`) |
| Provisioning request model | `ProvisioningRequest{OrgID,Type:"provision"|"reclaim",Status}` — `pkg/auth/models.go:88` |
| Mint join token (behind org-admin auth) | `handleDaemonJoinToken` — `pkg/orchestrator/server.go:408` |
| Join-token model (single-use, hashed) | `DaemonJoinToken` — `pkg/store/daemon_models.go` |
| Daemon self-registration | `handleDaemonRegister` → `RegisterDaemon` — `pkg/orchestrator/daemon_api.go`, `server.go:451` |
| Heartbeat: lease + seal per org | `handleDaemonHeartbeat` — `pkg/orchestrator/daemon_api.go:164` |
| Per-org lease w/ concurrency + budget caps | `LeaseNextTask(orgID, leasedBy, fleetID, ttl)` — `pkg/store/queue.go:87` |
| Fleet routing | `Daemon.FleetID` / task `fleet_id` — `pkg/store/daemon_models.go`, `queue.go:121-128` |
| Seal org vault to daemon key | `SealCredentialsForDaemon` — `pkg/store/credentials.go:74` |
| Daemon opens sealed creds in memory | `crypto.OpenSealed` — `pkg/daemon/daemon.go:296` |
| Actor/Critic runs in daemon; LLM key withheld from sandbox | `pkg/daemon/daemon.go:307-320,394-398,401-414` |
| Test command runs in sandbox, no network | `sandbox.RunCommand` w/ `NetworkNone:true` — `daemon.go:385-390`, `pkg/sandbox/exec.go` |
| PR published on green | `publishResult` — `pkg/daemon/daemon.go:502-508` |
| Store limits enforced by queue | `store.OrgLimits{MaxConcurrentJobs,MaxBudgetPerJob,…}` — `queue.go:97-119,147-175` |

**The one missing spine:** `ActivateOrg` writes `ProvisioningRequest` rows, but **nothing consumes them.** No process turns a pending request into a running per-org daemon. That is Phase 1.

**Known drift to respect:** two `OrgLimits` structs exist — `store.OrgLimits` (enforced by the lease queue) and `auth.OrgLimits` (`pkg/auth/limits.go`, different fields, enforced vs `task_states`). Free tier must set the **store** one. `pkg/sandbox/firecracker.go` is a non-viable prototype — do not build on it.

---

## Phase 0 — Verify the seam end-to-end (gate, not new work)

**Goal:** prove the existing register→heartbeat→lease→seal→open→execute→PR loop actually closes against real Postgres + a locally-run daemon, so later phases build on solid ground. CLAUDE.md §1 still claims the seam is unconnected — that doc is stale; confirm in reality.

**Do:**
1. Bring up the stack: `make run-local` (Postgres, NATS, MinIO), build the three binaries per CLAUDE.md §4 (external linking + `codesign -s -`).
2. Create an org + a `DaemonJoinToken` (via `handleDaemonJoinToken`, authed as org admin). Store an `ANTHROPIC_API_KEY` (or `GEMINI_API_KEY`) and a `GIT_TOKEN` credential for the org.
3. Run `kiwidaemon` with `-api-url` at the control plane; confirm it registers, then heartbeats.
4. `kiwi submit` a task against a small repo; confirm the daemon leases it, runs the Actor/Critic loop, runs the test in a sandbox, and opens a PR.

**Verification:** one task submitted from the CLI results in a real PR, with logs showing lease → `OpenSealed` → Actor/Critic → sandbox test → publish. Note any step that does *not* work as a Phase-0.5 bugfix before Phase 1. Update CLAUDE.md §1's stale "no task flows end-to-end" note as part of this phase.

**Note on model credits:** per project memory the Anthropic key is out of credits — use Gemini `gemini-flash-latest` for the live run.

---

## Phase 1 — Free provisioner + per-org daemon-process lifecycle (THE CORE GAP)

**Goal:** a `provision` `ProvisioningRequest` for a free org results in a running per-org daemon **process** (in a container) on a shared host, registered and heartbeating; a `reclaim` request stops it. Idle processes scale to zero; the next submit cold-starts one.

**What exists:** the request rows (`ProvisioningRequest`, `pkg/auth/activation.go`), the join-token mint + register + heartbeat endpoints. **Nothing reads the rows.**

**What to build (new package, suggested `pkg/provisioner`):**
1. A **poller** that selects `ProvisioningRequest` where `status = 'pending'`, oldest first, in a transaction with `FOR UPDATE SKIP LOCKED` (mirror `LeaseNextTask`'s locking, `queue.go:126-128`), and dispatches by `Type`.
2. A **Launcher interface** so the substrate is swappable and CI can stub it:
   ```go
   type Launcher interface {
       // Launch starts a per-org daemon process for orgID, bound to fleetID,
       // presenting joinToken on first handshake. Returns an opaque handle.
       Launch(ctx context.Context, orgID, fleetID, joinToken, apiURL string) (Handle, error)
       Stop(ctx context.Context, handle Handle) error
   }
   ```
   Ship a `DockerLauncher` (runs `kiwidaemon` in a container with a per-org cache volume) and a `StubLauncher` (records calls; used in tests). Do **not** require real Docker in CI.
3. **provision flow:** mint a join token for the org (reuse the join-token store path used by `handleDaemonJoinToken`; **do not** duplicate token logic), scoped to the shared-free `fleet_id`; `Launch` a daemon with it; mark the request `completed` (or `failed` with a reason).
4. **reclaim flow:** `Stop` the org's daemon; mark `completed`.
5. **Scale-to-zero + cold-start:** a free org's process is stopped after idle (reclaim). On the *next* `kiwi submit` for a stopped free org, enqueue a fresh `provision` request so a process cold-starts before/while the task waits in the queue. (The queue already holds the task until a daemon leases it — `ExpireStaleQueuedTasks` bounds the wait, `queue.go:442`.) Wire submit → "ensure a free daemon exists" without blocking the submit response.

**Files:** new `pkg/provisioner/`; touch `pkg/orchestrator/server.go` (submit path, near `402 Payment Required` gate at `server.go:566`, and task-create) to trigger cold-start; wire the poller into `cmd/kiwid` startup (orchestrator role).

**Tests:** poller consumes a pending `provision` row → `StubLauncher.Launch` called once with the org's id + shared-free fleet + a valid join token → row `completed`. `reclaim` → `Stop` called → `completed`. Concurrency: two pollers do not double-launch one request (SKIP LOCKED). Cold-start: submit for a stopped free org enqueues exactly one `provision`.

**Verification:** with the `DockerLauncher`, activating a free org spins up a `kiwidaemon` container that registers and heartbeats; suspending stops it; a submit after idle brings one back. No shared daemon anywhere — one process per org, each with its own keypair.

---

## Phase 2 — Tier wiring + free limits

**Goal:** signup → free org with the correct `store.OrgLimits` and its work routed to the shared-free fleet; submit is gated by plan; the `OrgLimits` drift is reconciled.

**What exists:** `Organization.Plan` (default `free`); `store.OrgLimits` enforced by the queue; `auth.OrgLimits` as a separate struct (`pkg/auth/limits.go`).

**What to build:**
1. **On org creation**, insert a `store.OrgLimits` free profile:
   ```go
   store.OrgLimits{
       OrgID:              org.ID,
       MaxConcurrentJobs:  1,
       MaxWorkersPerJob:   2,
       MaxBudgetPerJob:    0.50,   // guardrail on the USER's own spend (BYOK)
       MaxBudgetPerMonth:  0,      // not Kiwi's cost lever under BYOK; see Phase 4 for the real ceiling
       TaskTimeoutSeconds: 600,
       MaxSandboxDiskMB:   512,
   }
   ```
   Find the org-creation path (signup/auth) and add this insert in the same transaction.
2. **Fleet routing:** free orgs' daemons register into a well-known shared-free `fleet_id`; free tasks are enqueued with that `fleet_id` (or left unassigned if free daemons serve unassigned work — pick one and be consistent with `LeaseNextTask`'s routing at `queue.go:121-128`). Pro orgs pin to their dedicated fleet.
3. **Gate submit by plan:** ensure a free org can submit within its limits and a suspended/over-limit org gets a clear error (reuse the existing `402`/limits machinery, `server.go:566`, `queue.go:97-119`).
4. **Reconcile `OrgLimits` drift:** either make `auth.OrgLimits` a thin read-through to `store.OrgLimits`, or document one as canonical and stop writing the other. The queue enforces `store.OrgLimits`, so that is canonical — do not let `auth.OrgLimits` silently diverge. Add a test asserting a free org's effective limits are the ones the queue enforces.

**Tests:** new org → `store.OrgLimits` free row exists with the values above. Free task is routed to a daemon on the shared-free fleet and not to another org's fleet. Over-concurrency submit is refused (already enforced in `LeaseNextTask`; assert end-to-end).

**Verification:** a freshly created org has free limits, its task runs only on shared-free daemons, and a second concurrent job is held/refused per `MaxConcurrentJobs:1`.

---

## Phase 3 — gVisor isolation on the test sandbox

**Goal:** the untrusted test command (the only place model-generated code executes) runs under gVisor `runsc` on free hosts, behind a config flag, with Docker as fallback.

**What exists:** `sandbox.Driver` + `runDocker` (`pkg/sandbox/exec.go:88`) already builds `docker run` args with `--memory/--cpus/--network none`. `SandboxConfig` (`exec.go:22-29`) carries the knobs. `KIWI_SANDBOX=firecracker` toggles the prototype path (`exec.go:48-51`) — leave it, do not extend it.

**What to build:**
1. Add a runtime selector to `SandboxConfig` (e.g. `Runtime string // "runc" | "runsc"`), defaulting from an env/config so free hosts pick `runsc`.
2. In `runDocker`, when runtime is `runsc`, add `--runtime=runsc` to the `docker run` args (`exec.go:95`). Nothing else changes — gVisor is a drop-in OCI runtime.
3. Plumb the selection from the daemon's sandbox config (`daemon.go:385-390`) so free daemons request `runsc`.

**Tests:** unit test that `runDocker` emits `--runtime=runsc` when configured and omits it otherwise (assert on the built arg slice — refactor arg-building into a testable pure function if needed). Do **not** require gVisor installed in CI.

**Verification:** on a host with gVisor installed, a free task's test command runs under `runsc` (confirm via `runsc` process / container inspect); on a host without it, Docker fallback still works. Escape surface: the test command has a dedicated kernel and no network.

---

## Phase 4 — Abuse controls + metering

**Goal:** bound the one real free-tier abuse vector (CPU/cryptomining) and meter usage against a free ceiling. (Token cost self-limits under BYOK — see RFC §6.)

**What to build:**
1. **Hard per-task CPU + wall-clock cap** beyond the existing `TaskTimeoutSeconds`: enforce a CPU-seconds ceiling on the sandbox (cgroup/`--cpus` is already passed; add a wall-clock kill and a CPU-quota that a spin loop cannot exceed).
2. **Cryptomining signal:** flag tasks that burn CPU with little/no model-provider traffic from the daemon (the daemon makes the model calls, so "high sandbox CPU + few Actor/Critic calls" is observable daemon-side). Emit an abuse event; auto-suspend on threshold via `SuspendOrg`.
3. **Metering + free ceiling:** pick the unit (agent-minutes recommended — see RFC §10.4), accumulate per org per month, and refuse new leases when exceeded. Wire into the same place `LeaseNextTask` checks caps (`queue.go:97-119`) or a submit-time gate.

**Tests:** a task exceeding the CPU/wall-clock cap is killed and reported failed with a clear reason. A crossed free ceiling refuses the next submit/lease. Abuse signal fires on a synthetic "high CPU, no model calls" run.

**Verification:** a `while true` payload is killed by the CPU cap, not left running; an org past its monthly ceiling cannot start new work; suspension triggers a `reclaim`.

---

## Phase 5 — Optional hardening (defer until free ships)

Not required to launch; each is an independent upsell/security improvement.

1. **CP-side LLM gateway (zero model secrets on Kiwi hosts).** Point the daemon's provider `base_url` at a Control-Plane proxy that injects the org's model key server-side, so the key never leaves the CP. Removes the RFC §5 residue. Scope to free/managed only — **never** wire into BYOC (would break its privacy claim). Touches provider construction (`d.newProvider`, `daemon.go:403`) and adds a CP gateway endpoint.
2. **GitHub App JIT VCS tokens.** Replace the stored `creds["GIT_TOKEN"]` (`daemon.go:503`) with a GitHub App that mints a short-lived (1h), single-repo installation token per job, injected at publish time. Removes the last long-lived per-org secret from free hosts.

---

## Definition of done (whole effort)

A new signup can, with no contact-us step:
1. Land as `Organization{Plan:"free"}` with free `store.OrgLimits`.
2. Store their own model key (BYOK) + a VCS credential.
3. `kiwi submit` a task; a per-org daemon process cold-starts on a shared host, leases only their work, runs the Actor/Critic loop holding only their key, runs the test under gVisor with no network, and opens a PR.
4. Be bounded by CPU/wall-clock caps and a free ceiling; abuse auto-suspends and reclaims.
5. Upgrade to `Pro` → dedicated fleet, existing seal/lease paths unchanged.

No shared daemon. No queue changes. No new credential model. Everything reuses the seam that already ships.
