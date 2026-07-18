# RFC: Execution Model — Coordination, Context, and Multi-Tenant Isolation

**Date:** 2026-07-18
**Status:** Proposed
**Related:** [BYOC RFC](2026-07-16-startup-byoc-platform-rfc.md) · [Managed Execution Tier RFC](2026-07-17-managed-execution-tier-rfc.md) · [Architecture Review](../design/2026-07-16-byoc-architecture-review.md)

## 1. Summary

Five execution-model questions have been left implicit and have drifted apart from the original vision. This RFC decides them:

1. **Worker coordination** — who enforces the plan DAG and composes results. **Decision: the DAG is data, enforced by the Control-Plane scheduler; there is no runtime "master."**
2. **Result composition** — how N workers on one task produce one reviewable change, and how they share findings. **Decision: one job owns one git branch (shared code); dependency result-summaries carry findings.**
3. **Per-worker context** — the "persona / AGENT.md" idea. **Decision: no personas; a per-repo `AGENT.md` context file, plus scoped instructions on each worker.**
4. **Multi-tenant isolation** — how orgs are truly isolated, especially under the managed tier. **Decision: isolation is layered, and which layer carries cross-tenant isolation depends on deployment topology.**
5. **Parallelism** — the north-star goal of fire-and-forget concurrent tasks at scale. **Decision: the ceiling is inference, not sandboxes; the metric is verified throughput, not sandbox count; the levers are worker-slots per daemon, horizontal daemons per org, and a rate-limit-aware scheduler with enforced budget/concurrency caps.**

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

### Findings, not just code — how an `analyze` node feeds `impl` nodes

The branch composes *code*, but some nodes produce no code — an `analyze` worker's output is a set of *findings* ("the bug is in `Divide()`; three call sites assume non-nil"). How does `impl-users` see what `analyze` found?

**Decision: dependency result-summaries carry findings. Git carries code; the result channel carries context.** Each worker, on completion, reports a bounded, structured **result summary** back to the Control Plane (it already reports terminal status via `handleDaemonResult` — this extends that payload with a summary field). When the scheduler releases a task, it injects the result summaries of that task's `depends_on` predecessors into the worker's prompt.

This keeps each layer doing one thing and stays symmetric with Decisions 1 and 2:

- **Git branch = shared code state** (Decision 2). Durable, mergeable, the substrate of the PR.
- **Dependency result-summaries = shared findings.** Held in the CP alongside the task row, threaded by the scheduler — the same passive-infrastructure model as DAG enforcement (Decision 1). No blackboard store, no worker-to-worker channel, no master holding context.
- The summary is **bounded** (a size cap, enforced CP-side) so a fan-in node with many predecessors cannot blow its context window; when findings are large, the producing worker writes them to a file on the job branch and the summary points at the path.

An analyze-only node thus commits nothing to the branch but still hands its dependents exactly what they need. This is the mechanism the original "master coordinates the subagents" vision was reaching for — recovered as data flow through the scheduler, not a runtime process.

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

## 7. Decision 5 — Parallelism: the ceiling is inference, the metric is verified throughput

The stated goal is **fire-and-forget concurrent tasks at scale** — an engineer queues many tasks across many repos, walks away, and comes back to finished, reviewable work. This is the right ambition and the lease queue is already the right foundation for it. But the framing of "break the sandbox-throughput record" targets the wrong metric, and mis-targeting it would send engineering effort at a number that neither sells nor applies to this product. This decision names the real ceiling, the real metric, and the levers.

### The ceiling is inference, not sandboxes

The public records — 1M concurrent sandboxes, 100k in 24s — are **sandbox-infrastructure** benchmarks: a sandbox in them runs *code*, a compute problem solved by adding VMs. A Kiwi worker runs an *agent*, and every agent step is a frontier-model API call. That is a different physics:

- 1,000 concurrent sandboxes running a test suite is a compute problem.
- 1,000 concurrent agents is 1,000 streams of model calls against **one customer's API key**, which has a requests/min and tokens/min rate limit. On a typical provider tier, a few dozen concurrent agents begin to hit 429s.

So the practical concurrency ceiling on a single key is **tens, not thousands**, and no amount of sandbox engineering moves it — the bottleneck is the model provider, not the isolator. Chasing a sandbox-count record with an inference-bound product would spin up sandboxes that sit idle waiting on throttled inference. It is also **unsellable in BYOC**: a sandbox-per-second aggregate is a multi-tenant metric no single customer ever exercises in their own cloud.

### The metric is verified throughput

The defensible, differentiated metric is **verified, composed changes produced per hour** — not sandbox count. It is the metric the sandbox vendors (Modal, E2B, Northflank) *cannot* compete on, because they stop at "here is a sandbox, bring your own agent." Orchestration, DAG composition (Decision 2), and the test-gate (`test_cmd`, #120) are Kiwi's layer. Unverified output at scale is negative value — it relocates the bottleneck onto an exhausted reviewer — so the number that matters counts only changes a test suite has already gated green.

The sellable claim is therefore: *"Fire 50 tasks across your repos before you close your laptop; wake up to 40 green, merge-ready PRs and 10 flagged — each verified by your own tests."* Every number in it is a verified change, not an idle sandbox.

### The levers

Parallelism = **(daemons per org) × (worker-slots per daemon)**, bounded by inference and budget. Four concrete pieces, all building on the existing queue:

1. **Worker-slots per daemon.** Today `pollCP` processes leased specs sequentially — one daemon = one worker at a time. Give the daemon `K` concurrent slots and a bounded worker pool; it leases up to its free-slot count per heartbeat. The queue's `SKIP LOCKED` already makes concurrent leasing safe.
2. **Horizontal daemons per org.** N daemons draining one org's queue is already correct under `SKIP LOCKED` — no code change to the queue, only registration and scheduling account for it. This is the primary BYOC scale-out lever and the Managed RFC's step-1 escape from one-VM-per-org.
3. **Rate-limit-aware scheduler — the novel engineering.** The binding constraint is inference, so the scheduler must be inference-aware: track per-org (and per-key) provider budget, release tasks to keep the inference pipe saturated but under the rate limit, and back off on 429s instead of thrashing. In **managed**, it can pool multiple keys/providers and aggregate capacity across the fleet — which is precisely why massive parallelism is more achievable in managed than in BYOC. **This scheduler is the single piece of genuinely differentiated engineering in the system and is currently designed nowhere.**
4. **Enforced concurrency + budget caps.** `MaxConcurrentJobs` (defined on the org model, default 10) is **not enforced** on the lease path today, and `MaxBudgetUSD` exists per-job. Fire-and-forget without these produces surprise five-figure bills. Enforce an org-level in-flight-lease cap in `LeaseNextTask` and a per-job/per-org spend cap in the scheduler. Caps are what make "and forget" safe.

### Consequence

"Massive parallelism" is real and winnable — but it is won by scheduling around inference limits and composing verified output, not by spinning sandboxes. The metric, the demo, and the roadmap all point at the rate-limit-aware scheduler (lever 3) and the caps (lever 4), not at a sandbox-density benchmark.

## 8. Phased plan

Sequenced so each step is independently useful and nothing depends on unbuilt pieces.

1. **Enforce the DAG** (§3): add the dependency predicate to `LeaseNextTask`; a task is leasable only when its `depends_on` are `SUCCEEDED`. Small, high-value, unblocks correct ordering.
2. **Job-branch composition** (§4): one branch per job; workers check out and commit to it; terminal integration node opens the PR. This is the substance of "real multi-worker."
3. **Retire `pkg/agent`** (§3) once nothing references the master model.
4. **AGENT.md injection** (§5): read repo `AGENT.md`, prepend to worker prompts.
5. **Isolation hardening** (§6), in risk order: daemon-VM hardening → default-deny egress allowlist + credential-injecting proxy → microVM sandbox driver behind the existing `pkg/sandbox` interface → Postgres RLS.
6. **Turn on LLM planning**: wire a `Completer` adapter over a provider so `LLMPlanner` becomes a real path, and let it emit per-worker scope + `test_cmd`.
7. **Findings channel** (§4): extend the daemon result payload with a bounded summary; the scheduler injects a task's `depends_on` predecessors' summaries into its prompt. Small; unlocks analyze→impl handoff.
8. **Parallelism** (§7), sequenced cheap-to-hard: enforce the org concurrency cap and per-job budget cap in the lease path → `K` worker-slots per daemon → multi-daemon-per-org registration/accounting → the rate-limit-aware scheduler. The caps come first because they make everything after them safe to turn on.

## 9. Open questions

- **Branch conflict handling.** Concurrent independent workers on disjoint files merge cleanly; the planner must express would-conflict workers as dependencies. What happens when a commit *does* conflict — retry, serialize, or fail the job? Needs a policy.
- **Per-worker file scope enforcement.** §5 makes scope the real lever, but nothing yet *enforces* that a worker only edits its scoped files. Related to the `spec.File` path-traversal gap (a worker can currently write outside its worktree).
- **microVM vs. gVisor** for the density tier — isolation strength vs. I/O overhead; decide when the density tier is actually on the roadmap, not before.
- **Managed compute: buy vs. build** (carried from the Managed RFC §6) — renting E2B/Modal would supply the microVM layer without operating it. Spike before building a Firecracker driver.
- **Rate-limit discovery (§7).** The scheduler needs each org's real provider limits to pace against. Providers don't reliably advertise them; do we require the customer to declare their tier, probe adaptively from observed 429s/`retry-after` headers, or both?
- **Findings summary format (§4).** Is the result summary free text, or a small typed schema (findings, touched files, follow-ups) the planner and dependents can rely on? A schema is more useful to fan-in nodes but constrains the worker.
