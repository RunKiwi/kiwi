# Kiwi — Managed Tier on GCP Work Plan

> **Audience:** an implementation agent (the "worker"). Tasks are self-contained.
> Do them **in order**; respect `Depends on`. Open **one PR per phase**. A senior
> model verifies post-merge — ship tests first, write clear PR descriptions.
> Obey `CLAUDE.md`. Read the RFC before starting:
> **[docs/rfcs/2026-07-19-managed-tier-gcp-deployment.md](docs/rfcs/2026-07-19-managed-tier-gcp-deployment.md)**
> and its parent **[Managed Execution Tier RFC](docs/rfcs/2026-07-17-managed-execution-tier-rfc.md)**.

---

## 0. Orientation (READ FIRST — cold-start brief)

**What this plan is.** Deploy Kiwi's **managed tier** on Google Cloud: Kiwi runs
the Control Plane and one always-on Data Plane daemon **per org** in Kiwi's own
GCP project. The full task seam already works end-to-end (`make local`); this
plan operationalizes it on GCP and closes the security/correctness gaps that only
matter once *we* run untrusted, model-generated code and hold every secret.

**Non-negotiable decisions (from the RFCs — do NOT relitigate):**
- **One always-on daemon per org, hand-provisioned. NO autoscaler in v1.** The
  daemon is a stateful pet with a warm git cache; scale-to-zero defeats
  `pkg/gitcache`. (Managed RFC §3.2.1.)
- **Managed is NOT zero-knowledge.** Kiwi holds the X25519 private key and can
  decrypt. Never write code/docs/UI claiming zero-knowledge for managed.
- **Daemon VMs are hostile-by-default.** Untrusted code runs there; a sandbox
  escape lands in *our* GCP project. No ambient GCP creds, egress-allowlisted,
  segmented from the control zone. (Managed RFC §4.2.)
- **Do not build an isolation runtime.** Wrap Firecracker; don't write one.

**What already exists — do NOT rebuild:**
- `kiwid` has `-role api|orchestrator|all`, `/healthz`, `/readyz`.
- Lease queue is per-org in Postgres (`LeaseNextTask(ctx, orgID, leasedBy, …)`).
- Join tokens: `POST /api/v1/daemon/join-token` (org-scoped) already mints them.
- Sandbox is behind an interface (`pkg/sandbox`) with a Docker driver.
- Secrets encrypted at rest via `crypto.EncryptAtRest` (raw key today).
- `org_limits` (per-job budget, concurrency, task timeout) are **modeled** in
  `pkg/store`; managed must **enforce** them.

**Two kinds of task in this plan:**
- **Go/code tasks** — normal: tests first, all four pre-commit checks green.
- **Infra (Terraform) tasks** — live under `deploy/gcp/`. They can't be unit
  tested against real GCP in CI, so **acceptance = `terraform fmt -check` +
  `terraform validate` + a committed `terraform plan` output against a documented
  example tfvars**, plus a `deploy/gcp/README.md` runbook. Do not hard-code
  project IDs, regions, or secrets — everything is a variable.

**Pre-commit checks (run before every commit; all must pass):**
```bash
gofmt -l cmd/ pkg/           # prints nothing
CGO_ENABLED=0 go vet ./...
CGO_ENABLED=0 go test ./pkg/...
CGO_ENABLED=0 go build ./...
# Terraform phases additionally:
terraform -chdir=deploy/gcp/<module> fmt -check && terraform -chdir=deploy/gcp/<module> validate
```
Label each PR `skip-readme-check` unless it edits the root `README.md`.

---

## Phase G0 — Cloud-readiness of `kiwid` (code)

**Depends on:** nothing. **PR:** `feat(kiwid): migrate mode, structured logs, configurable pool`.

Cloud Run runs N replicas that must not race on migrations, and Cloud Logging
wants structured logs.

**Do:**
1. Add a **`-migrate` mode** to `cmd/kiwid` (or `-role migrate`): run
   `RunMigrations` and exit 0 (non-zero on failure). Serving roles must NOT run
   migrations on boot when a `KIWI_SKIP_BOOT_MIGRATE=true` env is set (default
   keeps today's behavior for `make local`). `/readyz` already checks the DB;
   ensure it returns 503 if a required table is missing.
2. **Structured logging:** introduce a small JSON logger (or `slog` with a JSON
   handler) and route `kiwid`'s logs through it — level, msg, and key fields
   (org_id, job_id, task_id where present). Keep it dependency-light.
3. **Configurable DB pool:** set GORM `SetMaxOpenConns`/`SetMaxIdleConns`/
   `SetConnMaxLifetime` from env (`KIWI_DB_MAX_OPEN`, …) with sane defaults, so
   Cloud Run fan-out cannot exhaust Cloud SQL connections.
4. Confirm graceful shutdown on SIGTERM (Cloud Run sends it): stop accepting,
   drain in-flight, exit.

**Acceptance:** `kiwid -migrate` applies migrations and exits; serving roles skip
boot-migrate under the env flag; logs are JSON; pool sizes honor env.

**Tests:** unit tests for the log formatter and the pool-config parsing; a test
that `-migrate` mode runs migrations against the SQLite/pg test harness and exits.

---

## Phase G1 — Terraform: Control Plane on GCP (infra)

**Depends on:** G0. **PR:** `infra(gcp): control-plane terraform`.

**Do:** create `deploy/gcp/control-plane/` (Terraform) provisioning:
- **Cloud SQL for Postgres** — private IP, HA, automated backups + PITR; a
  Serverless VPC Access connector for Cloud Run to reach it.
- **Artifact Registry** repo for images.
- **Secret Manager** entries for `KIWI_SERVER_TOKEN` and provider bootstrap keys.
- **Cloud KMS** keyring + key for encryption (consumed in G2).
- **Cloud Run**: `kiwid-api` (`-role api`, `min-instances=1`), `kiwid-orchestrator`
  (`-role orchestrator`, `min=max=1`, CPU always-on), `frontend`.
- **Cloud Run Job** `kiwid-migrate` (`-role migrate`), documented to run before
  traffic shifts.
- **Cloud Load Balancing** + Google-managed cert fronting api + frontend; drop
  Caddy. CORS via `KIWI_CORS_ALLOWED_ORIGINS`.
- A **`daemon` subnet** (segmented; used by G3) and the base VPC/firewall.
- All inputs as variables; a `terraform.tfvars.example`; a `README.md` runbook
  (build → push to Artifact Registry → migrate Job → deploy).

**Acceptance:** `terraform fmt -check` + `validate` pass; a committed `plan`
against the example tfvars; README runbook is followable. No secrets or project
IDs committed.

---

## Phase G2 — KMS-backed encryption + rotation (code)

**Depends on:** G1 (KMS key exists). **PR:** `feat(crypto): envelope encryption via Cloud KMS`.

`crypto.EncryptAtRest` uses a single symmetric key from an env var with no
rotation. Managed can't hold every org's creds behind an un-rotatable env key.

**Do:**
1. Introduce a `KeyManager` interface (Encrypt/Decrypt) with two implementations:
   the current env-key one (dev/local/BYOC) and a **Cloud KMS** one (envelope: a
   KMS key wraps per-write DEKs, or direct KMS Encrypt/Decrypt for small blobs).
   Select by env (`KIWI_KMS_KEY=projects/…/cryptoKeys/…` → KMS, else env key).
2. Store the wrapped DEK / KMS key-version alongside the ciphertext so **old
   versions still decrypt after rotation**.
3. A one-time **re-wrap migration** command that reads rows encrypted with the
   legacy env key and re-encrypts them under KMS.
4. Never log plaintext or keys.

**Acceptance:** with `KIWI_KMS_KEY` set, credentials round-trip through KMS;
without it, behavior is unchanged; rotating the KMS key still decrypts old rows.

**Tests:** a fake `KeyManager` in unit tests (no real KMS calls); round-trip and
rotation tests against the fake; the env-key path stays green.

---

## Phase G3 — Per-org daemon VM + provisioning (infra + small code)

**Depends on:** G1. **PR:** `infra(gcp): per-org daemon vm + provisioning`.

**Do:**
1. `deploy/gcp/daemon-vm/` Terraform module: one `e2-small`-class GCE VM in the
   daemon subnet, a persistent cache disk mounted at the daemon `-cache-dir`, and
   a **startup script** that installs the pinned `kiwidaemon` image/binary and
   runs it against the public API. The **join token arrives via instance
   metadata** (variable), never baked into an image. `enableNestedVirtualization`
   = true on the instance (so G6's Firecracker has `/dev/kvm`).
2. A thin **provisioning command** (`cmd/opsctl` or a `kiwid` admin subcommand):
   given an `org_id`, mint an internal join token (call the existing
   `POST /api/v1/daemon/join-token`) and emit the tfvars / run `terraform apply`
   for that org's VM. **Hand-run; no autoscaler, no controller.**
3. Runbook in `deploy/gcp/daemon-vm/README.md`: provision, verify the daemon
   registered + is leasing, and `destroy`/hibernate to reclaim.

**Acceptance:** `terraform validate` + committed `plan`; the provisioning command
mints a token and renders correct tfvars (unit-testable in Go: token minting +
tfvars templating, with the HTTP call stubbed).

**Tests (Go part):** the provisioning command's token request + tfvars rendering,
with the CP HTTP call stubbed via httptest.

---

## Phase G4 — Daemon VM hardening (infra)

**Depends on:** G3. **PR:** `infra(gcp): harden daemon vms (egress allowlist, no ambient creds)`.

Extend the `daemon-vm` module and VPC firewall so each VM is hostile-by-default
(Managed RFC §4.2, this RFC §5.3):

**Do:**
- Attach a service account with **no roles** (or no SA); block project SSH keys;
  disable OS Login; disable the default service-account token scope.
- **Shielded VM** on (secure boot, vTPM, integrity monitoring); serial console off.
- **Default-deny egress** firewall; allow only the public API LB, model API
  endpoints, and VCS — via **Cloud NAT** with an FQDN/CIDR allowlist. **No route
  to Cloud SQL, KMS, the control-zone services, or other daemon VMs.**
- No ingress. Document the allowlist and how to extend it for a new model host.

**Acceptance:** `terraform validate` + committed `plan`; README documents each
control and the threat it addresses; a reviewer can confirm no path from a daemon
VM to the control zone's private surface.

---

## Phase G5 — Idempotent PR creation (code) — correctness blocker

**Depends on:** nothing. **PR:** `fix(daemon): idempotent PR creation on redelivery`.

The lease queue is at-least-once: a crash after opening a PR but before reporting
redelivers the task, and today `publishResult` would open a **duplicate PR**. In
managed we are blamed for those.

**Do:** in `pkg/daemon/delivery.go`, before creating a PR, check for an existing
open PR whose head is the deterministic branch (`kiwi/<jobID>`); if the branch/PR
already exists, **adopt it** (push updates to the same branch, reuse the PR URL)
instead of creating a second. Report the same PR URL either way.

**Acceptance:** re-running delivery for the same job does not create a second PR.

**Tests:** extend `pkg/daemon` delivery tests with a fake GitHub client that
already has an open PR for the branch; assert no duplicate is created and the
existing URL is returned.

---

## Phase G6 — Firecracker sandbox driver (code) — M2 core

**Depends on:** G3 (nested-virt VMs). **PR:** `feat(sandbox): firecracker microVM driver`.

**Do:** add a **Firecracker driver behind the existing `pkg/sandbox` interface**,
selected by env (`KIWI_SANDBOX=firecracker|docker`, default `docker`). One
microVM per task: boot a minimal guest, mount the worktree, run the test command,
capture output + success, tear down. Keep Docker as the local/dev/BYOC default.
Do not change the `sandbox` interface or any caller.

**Acceptance:** with `KIWI_SANDBOX=docker` everything is unchanged; the Firecracker
driver satisfies the same interface and passes the shared driver contract test.

**Tests:** a driver-contract test table run against the Docker driver in CI; the
Firecracker path guarded behind a build tag / env so CI (no `/dev/kvm`) skips it,
with a documented manual verification on a nested-virt VM.

---

## Phase G7 — Egress proxy + credential injection (code) — M2

**Depends on:** G6. **PR:** `feat(daemon): credential-injecting egress proxy`.

The sandbox must **never hold the LLM key** (already true daemon-side for the
Actor), and egress must be allowlisted. `pkg/tunnel` is a head start.

**Do:** run a local proxy in the daemon that (a) injects provider/VCS auth headers
so the sandbox/test env never sees raw keys, and (b) allowlists egress to the
model endpoint + VCS only, denying everything else. Wire the sandbox's outbound
through it.

**Acceptance:** a task can reach the model/VCS through the proxy without the key
present in the sandbox env; non-allowlisted egress is refused.

**Tests:** proxy unit tests — header injection and allowlist enforcement — with
stub upstreams via httptest.

---

## Phase G8 — Enforce quotas & metering (code) — M3

**Depends on:** nothing. **PR:** `feat(limits): enforce org quotas + cost attribution`.

`org_limits` are modeled but not fully enforced. Managed (and any free tier) needs
hard limits and per-job cost attribution.

**Do:** enforce per-job budget, concurrency, and task timeout from `org_limits` at
lease/execute time (some already exist in `LeaseNextTask` — audit and complete);
attribute accumulated provider cost per job/agent and persist it; surface a clear
`result_detail` when a task is stopped by a limit.

**Acceptance:** a job exceeding its budget/concurrency/timeout is stopped with a
clear reason; cost is recorded per job.

**Tests:** queue/limits tests (extend `pkg/store` queue_limits tests) for each cap.

---

## Phase G9 — Protocol schema versioning (code)

**Depends on:** nothing. **PR:** `feat(daemon): heartbeat/worker-spec schema version`.

We upgrade managed daemons ourselves; BYOC daemons upgrade on their own cadence.
Without a version, the first breaking change strands BYOC daemons.

**Do:** add an explicit `schema_version` to the heartbeat request and
`worker-spec` payloads, and a min-supported check on the Control Plane (reject or
warn on too-old daemons with a clear message). Bump on breaking changes only.

**Acceptance:** a daemon sending an unsupported version gets a clear, actionable
rejection; current version flows unchanged.

**Tests:** handler tests for accepted / too-old versions.

---

## Phase G10 — Observability (code + infra)

**Depends on:** G0. **PR:** `feat(obs): opentelemetry + golden signals`.

**Do:** add an OpenTelemetry exporter (OTLP → Cloud Trace/Monitoring, configurable
endpoint) and emit golden-signal metrics: lease latency, loop duration, provider
cost, task success/failure rate, per-org concurrency. Document the dashboards/
alerts in `deploy/gcp/README.md`.

**Acceptance:** traces/metrics export to a configured OTLP endpoint; disabled
cleanly when unset (no-op, no crash).

**Tests:** exporter wiring unit test with a no-op/stub exporter; metric emission
covered where practical.

---

## Sequencing

- **Foundation:** G0 → G1 → G2. (Control Plane runnable on GCP with KMS.)
- **M1 (managed daemon operation):** G3 → G4.
- **Correctness blockers (interleave early):** G5, G9.
- **M2 (isolation):** G6 → G7.
- **M3 (metering/obs):** G8, G10.

Managed **v1 launch gate** = G0–G5 + G8 (enforced limits) with the Docker sandbox
under G4 hardening as a stated, time-boxed interim; G6/G7 (Firecracker + egress
proxy) land before density or before a public free tier, whichever comes first.

## Guardrails

- No real GCP/KMS/LLM/GitHub calls in tests — interfaces + fakes/httptest.
- Terraform: everything a variable; no project IDs/regions/secrets committed;
  `fmt`+`validate`+committed `plan` are the acceptance gate.
- Keep Go changes surgical and backward compatible — `make local` and BYOC must
  keep working (Docker sandbox, env-key crypto) with the new env unset.
- One PR per phase, tests first, `skip-readme-check` unless touching root README.
