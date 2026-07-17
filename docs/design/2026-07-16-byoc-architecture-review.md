# Architecture Review: Startup-First BYOC Platform RFC

**Date:** 2026-07-16
**Reviews:** [RFC: Startup-First BYOC Platform Pivot](../rfcs/2026-07-16-startup-byoc-platform-rfc.md)
**Related:** [Architecture](ARCHITECTURE.md) · [Phased Plan](../PHASED_PLAN.md) · [Phase 3 Completion Analysis](../../phase3_completion_analysis.md)

---

## 1. Overall Assessment

The architecture is sound. The pull-model BYOC split is the right call and follows a pattern proven by Buildkite agents, GitHub self-hosted runners, and Terraform Cloud agents:

- **Outward HTTPS polling** means no inbound ports into the customer VPC.
- **Dual-keypair design** (X25519 for credential sealing, Ed25519 for heartbeat signing) is honest cryptography — each primitive is used for what it can actually do.
- **Lease queue with dead-lettering** and the **transactional outbox** retained from the previous design are exactly the right consistency primitives.
- **Planner/worker model split** (frontier model for planning, cheap models for grunt work) is the correct cost structure for the Swarm.
- The **phasing** (data plane → control plane → integrations → distribution) was sequenced sensibly.

None of the findings below change the shape of the architecture. One contradiction should be resolved before more code is written (§2); the rest can be tracked as follow-up issues.

---

## 2. Critical: The Privacy Promise and the Planner Contradict Each Other

The RFC's core pitch is *"proprietary code never leaves their VPC"* (§2.1), but:

1. The **Planner runs in the Control Plane** and needs repo context to produce a meaningful DAG.
2. The current **`kiwi submit` zips the working directory and uploads it to the SaaS** (`cmd/kiwi/submit.go` → `sandbox.ZipDir`).

As designed and as built, code does leave the VPC — the flagship privacy claim is false on day one.

**Options:**

| Option | Description | Trade-off |
| :--- | :--- | :--- |
| **A. Daemon-side planning (recommended)** | The daemon calls the frontier model with a sealed planning key and sends only the resulting `worker-spec.json` metadata to the CP. | Makes the zero-knowledge story airtight; simplifies the CP into pure queue-and-state. Planning latency moves into the data plane. |
| B. Sanitized context upload | Daemon ships only file tree / symbol index to the CP planner, with explicit customer opt-in. | Weaker privacy guarantee; requires defining and defending what "sanitized" means. |

Either way, `kiwi submit` must become *"submit a task description; the daemon fetches the repo from VCS"* — the codebase-zip upload flow is a v0 holdover incompatible with the target architecture.

---

## 3. Security Gaps (cheap now, expensive later)

### 3.1 Sandbox credential exposure
The RFC decrypts keys "in memory during execution" (§4.1), but the execution agent inside the sandbox must call the Worker LLM somehow. If the key is env-injected into the container, any prompt-injected agent can exfiltrate it.

**Recommendation:** the daemon runs a local egress proxy that injects auth headers; the sandbox never holds the key. `pkg/tunnel` already exists — the enterprise JIT broker was paused in the pivot, but a stripped-down local injecting proxy is a fraction of that work and closes the worst hole.

### 3.2 Default-deny sandbox egress
Sandboxes run model-generated code inside the customer's VPC. Without a network allowlist (Worker LLM endpoint + VCS only, enforced via the same proxy), a hijacked agent can exfiltrate the entire repository. This is the difference between "BYOC is safer" being true and being marketing.

### 3.3 Daemon registration is trust-on-first-use
"Boot & self-register" as described lets anyone who discovers the registration endpoint enroll a rogue daemon and start receiving tasks.

**Recommendation:** the Terraform output includes a short-lived, org-bound join token that the first handshake must present. Key rotation and revocation also need a defined story before customers run daemons for months.

---

## 4. Correctness Details the RFC Glosses Over

### 4.1 Worktree claims are overstated (§4.2)
- *"Zero extra disk space"* is wrong: worktrees share the object store, not the checked-out tree. Each worktree materializes a full working copy.
- `git worktree add /tmp/task-123 main` **fails on the second concurrent task** — git refuses to check out the same branch in two worktrees. Use detached HEAD or per-task branches.
- A worktree's `.git` is a *file* pointing at the host-path gitdir. Mounting only `/tmp/task-123` into Docker gives the agent a broken git. Mount the shared gitdir read-only alongside, or restructure the layout.

### 4.2 At-least-once delivery means duplicate PRs
The lease queue redelivers on daemon crash. Without an idempotency check at the "create PR" step, retries open duplicate PRs. The previous design's side-effect ledger was dropped in the pivot — keep a minimal version scoped to VCS side effects.

### 4.3 Protocol versioning
Customers control their daemon upgrade cadence, not Kiwi. `worker-spec.json` and the heartbeat need an explicit schema version with a min-supported check from day one, or the first breaking change strands every deployed daemon.

---

## 5. Smaller Suggestions

1. **Budget caps** — per-task loop-iteration limits and per-org spend limits belong in `worker-spec.json` before "50 parallel agents overnight" meets a runaway loop on the customer's API key.
2. **Multiple daemons per org** — a single daemon VM is both a SPOF and the Swarm's parallelism ceiling. The lease queue already supports multiple consumers, so N daemons per org is nearly free; state it in the RFC.
3. **Customer-side log storage** — logs contain code. Store execution logs in the customer's own S3 with the CP holding only pointers, keeping the privacy story consistent end to end (also relevant to the SSE task-detail view, issue #74).

---

## 6. Suggested Priority

1. Resolve the planner/privacy contradiction (§2) — architectural decision, blocks further planner/submit work.
2. Credential-injecting egress proxy + default-deny networking (§3.1, §3.2).
3. Join-token registration handshake (§3.3).
4. Worktree concurrency/mount fixes (§4.1) and VCS side-effect idempotency (§4.2).
5. Protocol versioning (§4.3), budget caps, multi-daemon support, log locality (§5).
