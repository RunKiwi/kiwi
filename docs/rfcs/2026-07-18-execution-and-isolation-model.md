# RFC: Execution Model — Coordination, Context, and Multi-Tenant Isolation

**Date:** 2026-07-18
**Status:** Proposed
**Related:** [BYOC RFC](2026-07-16-startup-byoc-platform-rfc.md) · [Managed Execution Tier RFC](2026-07-17-managed-execution-tier-rfc.md) · [Architecture Review](../design/2026-07-16-byoc-architecture-review.md)

## 1. Summary

Four execution-model questions have been left implicit and have drifted apart from the original vision. This RFC decides them:

1. **Worker coordination** — who enforces the plan DAG and composes results. **Decision: the DAG is data, enforced by the Control-Plane scheduler; there is no runtime "master."**
2. **Result composition** — how N workers on one task produce one reviewable change. **Decision: one job owns one git branch; workers commit to it in dependency order.**
3. **Per-worker context** — the "persona / AGENT.md" idea. **Decision: no personas; a per-repo `AGENT.md` context file, plus scoped instructions on each worker.**
4. **Multi-tenant isolation** — how orgs are truly isolated, especially under the managed tier. **Decision: isolation is layered, and which layer carries cross-tenant isolation depends on deployment topology.**

## 2. Motivation: the drift

The pitch was: an LLM planner decomposes a task into a `worker-spec.json` DAG of N workers plus a **master** that coordinates them. What was actually built diverged:

- The planner *does* emit a DAG with `depends_on` (`pkg/planner`), and an `LLMPlanner` exists — but the running server uses the deterministic `HeuristicPlanner`, and the `LLMPlanner` has no model adapter wired, so LLM decomposition is scaffolding, not a path.
- **The DAG is stored but not enforced.** `LeaseNextTask` hands out the oldest `QUEUED` task and never consults `depends_on`. The heuristic plan's `verify` worker (which depends on all `impl` workers) can be leased and run *before any impl worker finishes*.
- **There is no runtime master.** A master/worker runtime exists (`pkg/agent`, issue #35) but is stranded off the BYOC path and unused.
- **There is no composition.** Each worker clones an isolated worktree at the same base ref (`pkg/daemon`), so worker B never sees worker A's edits. N workers produce N disconnected diffs.

So coordination is neither enforced (DAG ignored) nor active (no master), and results don't compose. This RFC resolves that rather than reviving the master.

## 3. Decision 1 — Coordination is data, not a process

**The plan DAG is the coordinator. The Control-Plane scheduler enforces it. There is no runtime master. `pkg/agent`'s master/worker runtime is retired.**

### Rationale

The system's strongest asset is the lease queue: pull-based, fenced (lease-id), crash-recoverable, dead-lettering. A runtime master fights it directly — a master is a *stateful central coordinator*, which reintroduces exactly the single-point-of-failure and checkpoint-the-coordinator problems the lease queue was built to avoid. In a distributed pull queue, a central brain is a liability.

Encoding `depends_on` in the plan already makes coordination **data**. Enforcing it is then a query predicate, not a process:

> A task is leasable only when every task in its `depends_on` is `SUCCEEDED`.

That predicate lives in strongly-consistent Postgres, recovers for free, and scales as a `WHERE` clause. `LeaseNextTask` gains a "no un-satisfied dependencies" condition; nothing else changes. A job's "master" becomes the scheduler — passive infrastructure, not an agent.

Aggregation ("the master collects results") is likewise handled without a process: the Control Plane knows a job is complete when all its workers reach a terminal state, and any final integration step is expressed as an ordinary DAG node that `depends_on` all others.

### Consequence

`pkg/agent` (Master/Worker, issue #35) is dead code on the target architecture and should be removed to stop implying a design we've decided against.

## 4. Decision 2 — One job owns one branch (the composition substrate)

The harder problem than "who coordinates" is: **how does worker B build on worker A's work?** Today it cannot — isolated worktrees at the same base ref.

**Decision: a job owns exactly one git branch (`kiwi/job-<id>`). Each worker, when the scheduler releases it, checks out the job branch — which already contains prior workers' commits — does its work, and commits back. The DAG ordering guarantees a worker only runs after its dependencies have committed.**

- **Git is the shared state.** No shared-memory coordination, no master holding a workspace.
- **The branch is the aggregation.** "Compose results" = the final state of the branch.
- **The DAG is the ordering.** Dependencies exist precisely so a worker sees its predecessors' commits.
- **One job = one branch = one PR.** A terminal integration/verify worker runs the full suite on the branch and opens a single PR.

This directly answers the review-bottleneck problem (see the GTM analysis): 50 workers no longer mean 50 diffs to review — they mean one branch, one PR, verified green by the terminal node.

Independent (non-dependent) workers editing disjoint files can still run concurrently and merge cleanly; workers that would conflict must be expressed as dependencies by the planner. Conflict handling on the branch is an open question (§8).

## 5. Decision 3 — Context, not personas

The vision was "N subagents with personas," plus an `AGENT.md`. These are two different ideas, and only one is worth building.

### Personas: no

Assigning role identities to workers ("you are a senior security engineer") is largely theater — the effect on real outcomes is marginal and inconsistent, and it invites a persona registry and taxonomy that carry complexity without paying for it. The real lever is **scoping**, not identity:

- a precise task,
- the file/directory scope the worker may touch,
- the tools it may use,
- the acceptance check (its `test_cmd`, from #120).

If the planner wants to specialize a worker's instructions, that is a scoped system/task prompt on the worker — not a first-class "persona" type. We will not build personas.

### AGENT.md: yes — but per-repo, not per-worker

`AGENT.md` is valuable, but not as a persona. Like `CLAUDE.md` / `.cursorrules`, it is a **per-repo context file**: conventions, how to run tests, what not to touch, domain notes. Its leverage is that it makes *every* worker on that repo better, it is authored once (by the customer, or learned and cached), and it is durable.

**Decision: the daemon injects the repo's `AGENT.md` (if present) into every worker's prompt for that repo.** It is not generated per-worker and not part of the plan DAG. The RFC's original sequence diagram conflated `AGENT.md` with per-worker persona; that was a category error.

## 6. Decision 4 — Multi-tenant isolation is layered by topology

"How do we truly isolate orgs, especially in managed?" The answer is four layers, and the crucial insight is that **the layer carrying cross-tenant isolation moves with deployment topology.**

| Layer | Isolates | Today | Target |
| :--- | :--- | :--- | :--- |
| **Data** | org rows in the CP | `org_id` on every row, app-level `WHERE` | + Postgres **row-level security** (a missed `WHERE` cannot leak across orgs) |
| **Compute** | one org's code/exec from another's | Docker (shared kernel) | **topology-dependent — see below** |
| **Network** | exfiltration by model-generated code | `NetworkNone` (breaks real builds) | **default-deny egress allowlist**: model endpoint + package registry + VCS |
| **Secrets** | injected credentials from the sandbox | git tokens env-injected; LLM key daemon-side (post-#120) | **credential-injecting proxy** — the sandbox holds no raw key |

### The compute layer, by topology

This is the crux, and the reason a single "use microVMs" answer is too blunt:

- **Managed v1 — one daemon VM per org (per the Managed RFC §3.2):** cross-org isolation *is the cloud VM boundary*. Org A and Org B run on different EC2/GCE instances, hardware-isolated by the cloud provider. Docker then only isolates *tasks within one org* — a same-tenant problem, for which a container is adequate. The managed-specific danger is not cross-tenant; it is **escape → the daemon VM's cloud identity → pivot into Kiwi's infrastructure.** So the daemon VM must be hardened: minimal IAM, no ambient cloud credentials, network-segmented from the control plane and other daemon VMs, outbound-only.

- **Managed at density — multiple orgs packed on one host (the margin play):** now orgs share a kernel, so cross-tenant isolation **must** move down to the sandbox. **microVM-per-sandbox (Firecracker/equivalent) becomes mandatory**; Docker is disqualifying here.

So: *v1 isolates orgs at the per-org VM boundary and hardens that VM; density isolates orgs at a per-sandbox microVM.* Naming which layer does the work at which scale is the whole answer — today it is undefined, which is the actual gap.

### Where the agent harness runs

Related, and decided here for consistency: the **agent harness runs inside the sandbox**, reaching the model through the egress proxy. The current daemon-side harness (#120) works only because the agent is a single-shot full-file rewriter — the moment the agent uses tools (bash, read, build), that untrusted, model-directed execution must be inside the microVM, not the daemon. v1's daemon-side harness is an explicit simplification, not the target.

## 7. Phased plan

Sequenced so each step is independently useful and nothing depends on unbuilt pieces.

1. **Enforce the DAG** (§3): add the dependency predicate to `LeaseNextTask`; a task is leasable only when its `depends_on` are `SUCCEEDED`. Small, high-value, unblocks correct ordering.
2. **Job-branch composition** (§4): one branch per job; workers check out and commit to it; terminal integration node opens the PR. This is the substance of "real multi-worker."
3. **Retire `pkg/agent`** (§3) once nothing references the master model.
4. **AGENT.md injection** (§5): read repo `AGENT.md`, prepend to worker prompts.
5. **Isolation hardening** (§6), in risk order: daemon-VM hardening → default-deny egress allowlist + credential-injecting proxy → microVM sandbox driver behind the existing `pkg/sandbox` interface → Postgres RLS.
6. **Turn on LLM planning**: wire a `Completer` adapter over a provider so `LLMPlanner` becomes a real path, and let it emit per-worker scope + `test_cmd`.

## 8. Open questions

- **Branch conflict handling.** Concurrent independent workers on disjoint files merge cleanly; the planner must express would-conflict workers as dependencies. What happens when a commit *does* conflict — retry, serialize, or fail the job? Needs a policy.
- **Per-worker file scope enforcement.** §5 makes scope the real lever, but nothing yet *enforces* that a worker only edits its scoped files. Related to the `spec.File` path-traversal gap (a worker can currently write outside its worktree).
- **microVM vs. gVisor** for the density tier — isolation strength vs. I/O overhead; decide when the density tier is actually on the roadmap, not before.
- **Managed compute: buy vs. build** (carried from the Managed RFC §6) — renting E2B/Modal would supply the microVM layer without operating it. Spike before building a Firecracker driver.
