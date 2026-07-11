# Distributed Agentic Execution Platform — Implementation Plan

> **For agentic workers:** This is a **program-level** plan. It decomposes the target-state RFC into phased workstreams with concrete modules, task checklists, interfaces, tests, and exit criteria. Each workstream is executed as its own detailed, bite-sized task plan (REQUIRED SUB-SKILL: superpowers:subagent-driven-development or executing-plans) written just-in-time when the workstream is picked up. Steps use `- [ ]` checkboxes for tracking.

**Goal:** Build the target-state distributed agentic execution platform from the RFC (`docs/rfcs/2026-07-10-agentic-execution-platform-rfc.md`) — a CP control plane that schedules isolated Actor–Critic agent sandboxes, with durable checkpoints, JIT secrets, multi-tenant governance, bring-your-own-LLM, and end-to-end observability.

**Architecture:** Two planes with one hard trust boundary. Control plane (stateless API servers + Postgres source-of-truth + transactional outbox → durable queue + LLM Orchestrator + manifest registry) provisions untrusted sandboxes (master/worker agents) through a pluggable Infrastructure driver; sandboxes talk back only through a narrow, job-scoped Agent API. Event-log + object-store checkpoints make runs resumable; a side-effect ledger makes replay safe.

**Tech Stack:** Go (control plane + sandbox agents), PostgreSQL (CP store), NATS JetStream (queue/event bus) via a transactional outbox, S3-compatible object store (snapshots/artifacts), Redis (rate-limit/cache/locks), Docker at v1 behind a pluggable Infra interface (→ gVisor/Firecracker/E2B/K8s), OpenTelemetry (traces/metrics/logs), gRPC internal + REST external. LLM: pluggable provider drivers (Anthropic, Codex, Gemini, any compatible endpoint).

## Global Constraints

- **Language/module:** Go; module `github.com/ibreakthecloud/kiwi` (current) — new services live under `pkg/` and `cmd/`.
- **Consistency:** CP — Postgres is the source of truth; queue handoff is exactly-once via transactional outbox; all state transitions are conditional (`UPDATE … WHERE status = expected`).
- **Isolation (v1):** Docker `--network=none` + cgroup limits behind the `Infra` interface; never widen without the interface.
- **Secrets:** never persisted in the sandbox; JIT broker (reverse tunnel / vault); provider keys encrypted at rest (AES-256-GCM), decrypted only at call time.
- **Multi-tenant:** every task-scoped row carries `org_id`; every read/action authorizes by org (or admin); no cross-tenant reach.
- **Observability:** every hop propagates W3C trace context; every agent turn/tool-call emits a structured event.
- **Honesty in naming:** provider set is **Anthropic, Codex, Gemini, or any compatible endpoint** — do not use the literal three-letter "Open"+"AI" brand string in code, config, or docs (use `codex`).
- **CI gate (every commit):** `gofmt -l`, `go vet ./...`, `go test ./...`, `go build ./...` all clean (see CLAUDE.md → Mandatory Pre-Commit Checks).
- **TDD:** each task ships with tests first; no networked tests in CI (stub providers/infra).

---

## 0. Baseline → Target (what exists vs what to build)

**Already shipped (prototype `main`):**
- Go daemon (`cmd/kiwid`) + CLI (`cmd/kiwi`); Actor–Critic engine (`pkg/orchestrator/engine.go`) with live Anthropic provider + mock; reverse-tunnel JIT secrets (`pkg/tunnel`); Docker sandbox (`pkg/sandbox`); **SQLite/GORM** persistence; multi-tenant auth/orgs/keys (`pkg/auth`); per-org limits + billing (`pkg/billing`); per-phase events (`pkg/orchestrator/events.go`); restart-recovery + idempotency; audit log; web console (monitoring).

**To build for target state (this plan):**
1. Swap **SQLite → Postgres** + **transactional outbox** + **durable queue** (NATS JetStream).
2. Split the monolith daemon into **API server / LLM Orchestrator (queue consumer)** roles.
3. **Manifest & workflow registry** (immutable, content-addressed) + manifest-driven provisioning.
4. **Pluggable Infra driver** (formalize Docker; add gVisor/Firecracker/E2B/K8s later).
5. **Master–Worker** sandbox topology + narrow **Sandbox Agent API** (gRPC, scoped token).
6. **Checkpoint/State service** (event log + S3 snapshots) + **side-effect ledger** + rollback/resume.
7. **Multi-provider LLM layer** (Anthropic/Codex/Gemini/compatible) + per-role model selection.
8. **OpenTelemetry** tracing/metrics + **event bus → SSE** live streaming.
9. **Hardening**: scoped sandbox tokens, egress allowlists, manifest signing, provider isolation.

---

## Build-order dependency graph

```
P1 Foundation (CP correctness)         P2 Agents + durability
┌───────────────────────────┐         ┌──────────────────────────────┐
│ Postgres store  ─┐         │         │ Master/Worker ─┐             │
│ Transactional outbox ─┐    │         │ Agent API (gRPC) ─┐          │
│ NATS queue        ─┐  ├───▶│────────▶│ Event log         ├─▶ Checkpoints/S3 ─▶ Rollback │
│ API server role  ─┘  │    │         │ Side-effect ledger┘          │
│ LLMO consumer + manifest ─┘         │ Boot recovery (Postgres)     │
└───────────────────────────┘         └──────────────────────────────┘
        │                                        │
        ▼                                        ▼
P3 Security hardening ───────────────▶ P4 Observability & scale ───▶ P5 Governance & extensibility
(JIT broker, scoped tokens,           (OTel end-to-end, event bus     (BYO-LLM providers, budgets/quotas
 egress allowlist, manifest sign,      → SSE, metrics/dashboards,      UI, template authoring, plugins,
 gVisor/Firecracker driver)            per-tenant fair queuing, K8s)   cross-provider routing)
```

Each phase produces working, demoable software. Do not start a phase until the prior phase's exit criteria pass.

---

## Phase P1 — Control-plane foundation (correctness & durability spine)

**Objective:** replace the in-process SQLite monolith with a CP control plane: Postgres source-of-truth, exactly-once queue handoff, an API-server role and an LLMO consumer role, and manifest-driven single-agent runs. End state: a job submitted via API is durably persisted, exactly-once handed to the LLMO, compiled to an immutable manifest, and run in one Docker sandbox — with strong no-double-schedule guarantees.

### Workstream P1.1 — Postgres store + migrations
- **Modules:** `pkg/store/` (new; GORM or `pgx` + `sqlc`), `cmd/kiwid` wiring.
- **Tasks:**
  - [ ] Introduce a `Store` interface abstracting task/manifest/event/checkpoint/outbox reads+writes; implement `PostgresStore`.
  - [ ] Port the schema in RFC §5 to Postgres migrations (`migrations/00xx_*.sql`): `organizations, org_limits, workflows, manifests, jobs, agents, outbox, events, checkpoints, side_effects, audit_logs` — with the RFC's indexes and the `jobs (org_id, idempotency_key)` unique constraint.
  - [ ] Keep a `SQLiteStore` behind the same interface for local/offline tests (or use a throwaway Postgres via testcontainers).
- **Produces:** `store.Store` interface; `jobs.status ∈ PENDING|SCHEDULING|RUNNING|PAUSED|SUCCEEDED|FAILED|CANCELED`.
- **Tests:** migration up/down; unique-constraint dedupe; conditional status transition (`WHERE status = expected` returns rows-affected 0 on stale).
- **Exit:** all existing orchestrator tests pass against `PostgresStore`.

### Workstream P1.2 — Transactional outbox + relay
- **Modules:** `pkg/outbox/`.
- **Tasks:**
  - [ ] On job create, write `jobs` row + `outbox` row in **one transaction**.
  - [ ] Relay process: poll `outbox WHERE published_at IS NULL`, publish to the queue, mark published; at-least-once with dedupe by `job_id`.
  - [ ] Idempotent consumer guard in LLMO (conditional update).
- **Produces:** `outbox.Relay`; topic `jobs.submitted`.
- **Tests:** crash between commit and publish → relay re-publishes; duplicate delivery → LLMO no-op.
- **Exit:** kill-9 the relay mid-publish; every committed job is delivered exactly once (effect).

### Workstream P1.3 — Durable queue (NATS JetStream)
- **Modules:** `pkg/queue/` (driver interface + JetStream impl; in-memory impl for tests).
- **Tasks:**
  - [ ] `Queue` interface: `Publish(topic, msg)`, `Subscribe(topic, group, handler)` with ack/nack + visibility timeout + DLQ.
  - [ ] Per-tenant fairness stub (weighted subjects) — full fair queuing lands in P4.
- **Produces:** `queue.Queue`.
- **Tests:** redelivery on nack; DLQ after N retries (in-memory impl).
- **Exit:** a message survives a consumer restart and is redelivered.

### Workstream P1.4 — API server role
- **Modules:** split `pkg/orchestrator/server.go` → `pkg/api/` (HTTP edge) vs `pkg/orchestrator/` (LLMO). `cmd/kiwid` gains a `-role api|orchestrator|all` flag.
- **Tasks:**
  - [ ] `POST /jobs` (auth, validate, quota, `Idempotency-Key`, outbox write) — reuse existing auth/limits/idempotency.
  - [ ] Read APIs: `GET /jobs`, `GET /jobs/{id}` (org-scoped), `GET /jobs/{id}/events`.
  - [ ] Stateless; horizontally scalable; no sandbox or engine calls in this role.
- **Produces:** the external REST surface.
- **Tests:** httptest — quota rejection, idempotent dedupe, org-scoped 403.
- **Exit:** API server runs with zero engine imports; submit → row + outbox only.

### Workstream P1.5 — LLMO consumer + manifest generation + single-agent run
- **Modules:** `pkg/orchestrator/` (consumer loop), `pkg/manifest/` (registry).
- **Tasks:**
  - [ ] LLMO consumes `jobs.submitted`, resolves workflow/template, generates an **immutable content-addressed manifest** (id = sha256(content)), persists it, pins `manifest_id` on the job.
  - [ ] Provision via the Infra driver (P1.6), run the existing Actor–Critic engine for a single agent, finalize state/cost.
  - [ ] Boot-recovery sweep over `RUNNING|PAUSED|PENDING` rows (Postgres) — re-attach or fail (port existing `RecoverTasks`).
- **Produces:** `manifest.Registry`, `manifest.Generate(workflow, inputs) → Manifest`.
- **Tests:** deterministic manifest hash; recovery classification; consumer idempotency.
- **Exit:** end-to-end job via API → queue → LLMO → manifest → sandbox → SUCCEEDED, no double-schedule under duplicate delivery.

### Workstream P1.6 — Infrastructure driver interface (formalize Docker)
- **Modules:** `pkg/infra/` (interface + `DockerInfra`).
- **Tasks:**
  - [ ] `Infra` interface: `Provision(ctx, Manifest) (Handle, error)`, `Status`, `Snapshot`, `Restore`, `Terminate`.
  - [ ] Wrap existing `pkg/sandbox` Docker exec behind `DockerInfra`; manifest supplies limits + egress policy.
- **Produces:** `infra.Infra`, `infra.Handle`.
- **Tests:** provision/terminate lifecycle; limits applied (`--network=none`, cpu/mem).
- **Exit:** LLMO provisions only through `Infra`; no direct Docker calls elsewhere.

**P1 exit criteria (milestone M1 — "durable single-agent runs"):** submit→run works through the split roles; Postgres is source of truth; outbox gives exactly-once; manifests are immutable/replayable; boot recovery leaves no zombies; scheduling-latency instrumented (target p95 < 5s).

---

## Phase P2 — Master/Worker agents + checkpointing (resumability)

**Objective:** upgrade single-agent runs to a Master–Worker topology behind a narrow Sandbox Agent API, add the durable event log + object-store checkpoints + side-effect ledger, and deliver deterministic rollback/resume.

### Workstream P2.1 — Sandbox Agent API (gRPC, scoped token)
- **Modules:** `pkg/agentapi/` (proto + server in CP, client in sandbox); `cmd/kiwi-agent/` (in-sandbox binary).
- **Tasks:**
  - [ ] Define proto: `AppendEvent`, `Checkpoint`, `FetchSecret`, `ReportResult`.
  - [ ] Per-job **short-lived scoped token** minted at provision; server authorizes it can only write its own `job_id`.
- **Produces:** `agentapi` client/server; scoped-token issuance.
- **Tests:** token scoped to job (cross-job write → PermissionDenied); token TTL/expiry.
- **Exit:** sandbox reaches the CP only through this API with a scoped token.

### Workstream P2.2 — Master + Workers
- **Modules:** `pkg/agent/master/`, `pkg/agent/worker/` (run inside the sandbox).
- **Tasks:**
  - [ ] Master decomposes the goal, spawns N workers (static per manifest v1), mediates messaging, aggregates, reports terminal status.
  - [ ] Worker runs a scoped subtask (per-worker model/tools from manifest); crash-isolated.
- **Produces:** master/worker runtime; `agents` rows per run.
- **Tests:** master coordinates ≥2 workers (mock provider); worker crash → recoverable.
- **Exit:** a multi-agent run completes; per-agent events recorded.

### Workstream P2.3 — Event log + object-store checkpoints
- **Modules:** `pkg/checkpoint/` (+ `pkg/objstore/` S3 client).
- **Tasks:**
  - [ ] Append-only `events` (per-job monotonic `seq`); small structured `checkpoints` in Postgres; workspace snapshot to S3 (content-hashed) at each agent-turn boundary + before side-effecting tools.
  - [ ] Snapshot/restore via `Infra.Snapshot/Restore`.
- **Produces:** `checkpoint.Service.Write/Latest/Restore`.
- **Tests:** checkpoint round-trip (write → restore identical workspace hash); cadence honored.
- **Exit:** a run produces an ordered event log + at least one restorable snapshot.

### Workstream P2.4 — Side-effect ledger + rollback/resume
- **Modules:** `pkg/checkpoint/ledger.go`, LLMO resume path.
- **Tasks:**
  - [ ] Before a side-effecting tool call, consult ledger keyed `hash(job_id, seq, effect_signature)`; short-circuit if committed (return cached result).
  - [ ] Rollback algorithm (RFC §7.3): latest checkpoint → restore → replay event tail → resume; replay never double-fires effects.
- **Produces:** `ledger.Check/Commit`; `LLMO.Resume(job)`.
- **Tests:** replay after crash does not re-fire a recorded effect; resume converges to SUCCEEDED.
- **Exit:** kill a run mid-loop; on restart it restores + replays + finishes with no duplicate side effects.

**P2 exit criteria (milestone M2 — "resumable multi-agent runs"):** master/worker runs are observable per-agent, checkpointed, and deterministically resumable; sandbox↔CP is a scoped-token gRPC boundary.

---

## Phase P3 — Security & trust boundary hardening

**Objective:** make the two-plane boundary production-safe.

### Workstreams
- **P3.1 JIT secret broker** (`pkg/secrets/`): formalize reverse-tunnel + add a vault/KMS driver for service creds; secrets injected at egress, never at rest in sandbox; pause-on-unavailable.
  - Tests: secret never written to sandbox FS; pause/resume on reconnect. Exit: no long-lived secret in any sandbox.
- **P3.2 Egress allowlist enforcement** (`pkg/infra`): deny-by-default; manifest declares allowed hosts; driver/network policy enforces.
  - Tests: undeclared host blocked; declared host reachable. Exit: SSRF/exfil blocked by default.
- **P3.3 Manifest integrity**: content-addressed + JSON-Schema validation on ingest; optional signature for 3rd-party/plugin producers; LLMO rejects invalid.
  - Tests: tampered manifest rejected; unsigned plugin manifest rejected when signing required. Exit: only validated manifests provision.
- **P3.4 Stronger isolation driver**: add `GvisorInfra` and/or `FirecrackerInfra` behind `Infra`; select per manifest/env.
  - Tests: parity provision/terminate; syscall/VM isolation smoke. Exit: hostile-tenant runs can select microVM isolation.

**P3 exit (milestone M3 — "safe for untrusted multi-tenant"):** threat-model review passes; a fully-compromised sandbox is bounded to its own job data, declared egress, and its token TTL.

---

## Phase P4 — Observability & scale

**Objective:** full legibility + horizontal scale.

### Workstreams
- **P4.1 OpenTelemetry end-to-end** (`pkg/telemetry/`): W3C trace context CLI→API→queue headers→LLMO→Agent API→per-agent span; span model `job→schedule→sandbox→agent→turn→tool_call`; `events.trace_id/span_id` correlation.
  - Tests: one trace_id spans a full job (in-proc exporter). Exit: 100% of agent turns/tool-calls emit spans.
- **P4.2 Event bus → live streaming**: publish events to the bus; `GET /jobs/{id}/stream` (SSE/WebSocket) with reconnect-by-seq + dedupe; durable copies in `events`/S3.
  - Tests: SSE delivers ordered events; reconnect fills the gap by seq. Exit: live per-step UI works; no lost events on reconnect.
- **P4.3 Metrics & dashboards**: RED (API/queue/LLMO) + USE (sandboxes) + domain metrics (cost/tokens per job/tenant, checkpoint overhead); Grafana deep-links (don't rebuild in-app).
  - Exit: SLO dashboards (submit p95, scheduling p95, availability) live.
- **P4.4 Fair queuing + backpressure + DLQ ops**: per-tenant weighted fair queuing; global admission control; DLQ inspector (operator-only).
  - Tests: noisy tenant can't starve others. Exit: overload degrades via backpressure, not failure.
- **P4.5 Fleet-scale Infra drivers**: `K8sInfra` (Jobs) and/or E2B driver for elastic sandbox capacity.
  - Exit: sustain the RFC 12-month concurrency target (5k concurrent sandboxes) in a load test.

**P4 exit (milestone M4 — "observable & scalable"):** distributed tracing + live streaming + metrics complete; load test meets SLOs.

---

## Phase P5 — Governance & extensibility (incl. Bring-Your-Own-LLM)

**Objective:** productize governance and the pluggable surfaces.

### Workstreams
- **P5.1 Multi-provider LLM layer (BYO-LLM)** (`pkg/provider/`): formalize the `LLMProvider` driver; add **Anthropic, Codex, Gemini**, and a **compatible-endpoint** driver (base-URL + key); per-role Actor/Critic model selection from the manifest; normalized per-provider cost accounting; optional cross-provider fallback. Per-org keys encrypted at rest (extend existing provider-config), decrypted only at call time.
  - Tests (no network): each driver builds/validates params; cost normalization; per-role selection; fallback on primary error (stubbed). Exit: a run can use different providers for Actor vs Critic.
- **P5.2 Budgets/quotas + cost UI**: surface per-org limits editing + monthly budget bar truthfully (from `org_limits`); enforce before enqueue (already partly shipped).
  - Exit: admins edit limits; budget bar reflects real caps.
- **P5.3 Template authoring (CLI-first) + workflow registry APIs**: `GET /workflows`, create/update via CLI, JSON-Schema-validated `spec`.
  - Exit: users author + select templates; manifests reference them.
- **P5.4 3rd-party manifest plugins**: accept plugin-produced manifests validated (and optionally signed) against the schema.
  - Exit: an external plugin can submit a valid manifest.

**P5 exit (milestone M5 — "GA-ready control plane"):** BYO-LLM live; governance self-serviceable; extensibility surfaces documented.

---

## Cross-cutting: testing & CI strategy

- **Unit (default):** pure logic + interface stubs (mock provider, in-memory queue/store, fake Infra). No network. This is the CI gate (`go test ./...`).
- **Integration:** Postgres + NATS + MinIO via testcontainers for store/outbox/checkpoint/queue workstreams; tagged `//go:build integration`, run in a dedicated CI job.
- **Contract:** proto/API golden tests for `agentapi` and REST.
- **E2E (mock mode):** submit→run→checkpoint→resume with the mock provider; runs in CI nightly.
- **Load (P4):** k6/vegeta against the API + a synthetic Infra to hit the concurrency SLO.
- **Security (P3):** egress-block, scoped-token, secret-at-rest negative tests.
- **Every commit:** `gofmt -l`, `go vet ./...`, `go test ./...`, `go build ./...` green (CLAUDE.md gate).

## Milestones summary

| Milestone | Phase | Demoable outcome |
|---|---|---|
| M1 Durable single-agent runs | P1 | API→queue→LLMO→manifest→sandbox, Postgres CP, exactly-once, no zombies |
| M2 Resumable multi-agent runs | P2 | Master/worker + event log + S3 checkpoints + deterministic rollback |
| M3 Safe multi-tenant | P3 | JIT secrets, scoped tokens, egress allowlist, microVM option, manifest integrity |
| M4 Observable & scalable | P4 | OTel tracing + live SSE + metrics; meets SLOs under load |
| M5 GA control plane | P5 | Bring-your-own-LLM (Anthropic/Codex/Gemini/compatible), governance, plugins |

## Risks & sequencing notes
- **SQLite→Postgres is load-bearing for P1**; do it first — every later guarantee depends on transactional outbox + conditional updates.
- **Determinism limits:** non-deterministic tools (network/time) mean "resume, not bit-exact replay"; the side-effect ledger is what preserves correctness — invest test effort there.
- **Snapshot cost** grows with checkpoint cadence; add dedup + retention/GC in P2, revisit in P4.
- **Don't build observability the RFC delegates** (trace waterfalls, metrics dashboards) into bespoke UI — deep-link Grafana/Tempo.
- **Keep the Infra + LLMProvider interfaces stable early**; every later driver depends on them not churning.

## Execution model
Pick a workstream in dependency order; write its detailed bite-sized task plan (failing test → minimal impl → passing test → commit) via **superpowers:writing-plans**, then execute via **superpowers:subagent-driven-development** (fresh subagent per task + review) or **executing-plans**. Each workstream ends green on the CI gate before the next begins.
