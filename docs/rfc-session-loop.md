# RFC: The Session Loop — a persistent Architect and an agentic Implementer

Status: proposal
Supersedes: nothing yet — ships alongside `pkg/loop` (see §9)

This proposes replacing the *shape* of Kiwi's execution loop, not its trust
model. Today one LLM call decomposes a task into file-assigned workers, and each
worker runs a single-turn Actor that rewrites one file whole. This proposes one
persistent **Architect** that owns a task from prompt to PR, and one **agentic
Implementer** that does real tool-driven work against the repository, iterating
in reviewed rounds.

Everything below is written against the code as it exists: `pkg/loop/loop.go`,
`pkg/planner`, `pkg/provider`, `pkg/daemon/daemon.go`, `pkg/store/queue.go`,
`pkg/sandbox/exec.go`.

---

## 0. The one fact that shapes every answer

**There is no tool-calling anywhere in this codebase.** `provider.Provider` is
`GetCodeEdit` + `Complete`, both single-turn; a grep for `tool_use`, `ToolParam`,
or `FunctionDeclaration` across `pkg/` and `cmd/` returns nothing. And
`sandbox.RunCommand` is one-shot: it shells out to `docker run --rm` per call
(`pkg/sandbox/exec.go:99`), so forty tool calls would be forty container starts.

So the interesting question is not "how do we restructure the loop." It is "what
is the smallest set of new primitives that makes an agentic implementer possible,
and what do they do to the invariants Kiwi already holds?" Two primitives are
new: a **tool-calling provider seam** and a **persistent sandbox session**.
Everything else in this document is composition.

---

## 1. Roles and the state each one owns

| Role | Runs where | Model tier | Lifetime | Owns |
|---|---|---|---|---|
| **Architect** (C1) | Daemon process | Expensive (Opus-class) | Whole task | The spec, the acceptance criteria, memory of what was tried and rejected, the final PR body |
| **Implementer** (C2) | Daemon process; **tools** execute in the sandbox | Cheap (Sonnet/Gemini-class) | One round | Nothing durable. Its output is commits on the job branch plus a handoff note |
| **Session driver** | Daemon process | — | Whole task | Round sequencing, budgets, rails, the event log, git |
| **Sandbox session** | Container (gVisor) | — | One round | The worktree and the toolchain. No credentials (§4) |

The Architect is the only stateful conversation. The Implementer is deliberately
not one — see §2.

`Session driver` is the new `pkg/session` package, and it occupies exactly the
position `loop.Runner` occupies today: the daemon injects how tests run and where
credentials come from, and the package itself imports no sandbox or store
dependency. That property in `pkg/loop`'s header comment is worth preserving.

---

## 2. C2 statefulness: **fresh context per round**

Decision: **the Implementer gets a fresh context every round.** Not stateful.

The usual framing of this tradeoff — re-exploration cost versus context drift —
undersells the decisive point, which is:

> In an agentic coder, the transcript is a lossy, stale cache of the filesystem.
> The filesystem is durable and authoritative.

A stateful C2's context contains file contents it read in round 1 and then
*edited* in round 1. Those reads are now wrong. It must re-read them to act
safely, so statefulness does not save the re-read; it just adds a stale copy
above the fresh one and invites the model to trust the wrong one. This is not
hypothetical for Kiwi specifically: the Architect rejects a round *because the
approach was wrong*, and a stateful C2 carries its full defence of that approach
into the retry. It patches instead of reconsidering.

What a fresh round is actually seeded with, so "fresh" is not "cold":

1. The **Architect's spec** for this round (structured; §5).
2. A **repo map** — the file tree plus a cheap symbol index, computed once per
   session by the daemon (not the model), reused verbatim every round. `pkg/daemon`
   already has `repoTree` for exactly this.
3. The **diff so far**: `git diff <base>..<head>` on the job branch. This is the
   real memory of prior rounds, and it lives in git, where it is correct.
4. The previous round's **handoff note** — 20–40 lines the Implementer writes as
   its last act ("the parser lives in X, the test harness needs Y, I could not
   find Z"). Cheap, model-authored, and the only part of the transcript worth
   keeping.

Re-exploration cost, priced: a fresh round re-runs perhaps 15–25 read/grep calls
before it starts editing. That is maybe 30k input tokens, and at the cached rate
(§6) it costs cents. Context drift, by contrast, costs a wasted round — hundreds
of times more.

**Caching economics, both directions.** Prompt caching pays *within* a round and
barely pays *across* rounds. Within a round the transcript grows monotonically:
turn N re-reads every prior turn, so a cache breakpoint that advances with the
transcript turns an O(n²) input bill into an O(n) one. That is where the money
is (§6), and it is available to fresh-per-round exactly as much as to stateful.
Across rounds the gap is a verification run plus an Architect review — routinely
minutes — so a 5-minute ephemeral cache has usually expired, and the only thing
worth holding on a longer TTL is the static prefix (system prompt, tool
definitions, repo map), which fresh-per-round keeps byte-identical *by
construction*. A stateful C2 would be paying to keep a transcript warm whose
value is negative.

And the clinching argument is §3: fresh-per-round makes the round the atomic unit
of work, which is the only reason this design fits Kiwi's lease queue at all.
The answer to question 1 and the answer to question 3 are the same answer.

---

## 3. Does the DAG survive? **No — in this mode it collapses to one worker**

Decision: in session mode the planner emits **one worker per job**. One task, one
session, one branch, one PR. Parallel file-assigned workers go away.

This is less of a demolition than it sounds, because today's parallelism is
already largely nominal:

- `Plan.Validate` (`pkg/planner/planner.go:157`) rejects two workers that touch
  the same file without a dependency path between them. The DAG's file
  partitioning is a *conflict-avoidance mechanism*, not a throughput mechanism.
- The daemon already forces one job onto one branch — `jobBranchName` is
  `kiwi/<jobID>` and `GetJobWorktree` bases each worker on the shared job branch
  (`pkg/daemon/daemon.go:439`). Concurrent workers on one branch either serialize
  or race; the dependency edges exist to make them serialize.
- The planner assigns files **without ever seeing the repository**. The daemon
  says so in its own comment (`daemon.go:527`): "A path on the spec is a hint from
  the planner — which is given the repo URL, not its contents." An entire repair
  pipeline exists downstream — `resolveHint`, `discoverTargetFiles`,
  `correctNewFileExtension` — to fix guesses made by a component that cannot look.

An Implementer that can `grep` deletes the need for all of it. The planner stops
guessing files because nothing downstream needs a guess.

**Where parallelism actually survives**, and it should:

1. **Across jobs.** The lease queue, the org concurrency cap
   (`limits.MaxConcurrentJobs`), and fleet routing are untouched. Ten users'
   tasks still run on ten daemons at once. This was always the parallelism that
   mattered commercially.
2. **Read-only fan-out inside a round.** The Implementer may issue several
   `grep`/`read` tool calls in one turn, executed concurrently by the driver.
   Safe because they mutate nothing.
3. **Deferred: disjoint milestones.** If the Architect can *declare* two
   milestones file-disjoint, the driver could run two Implementer rounds
   concurrently on separate worktrees and merge. This is the only place real
   intra-task parallelism belongs, it inherits every conflict problem the current
   DAG has, and it should not be built until the sequential path is proven. Phase 5.

The DAG code does not get deleted — `HeuristicPlanner`, `Validate`, and the
`depends_on` gating in `LeaseNextTask` all remain live for `file_loop` mode.

### 3.1 What the Control Plane planner does in session mode

"The planner stops calling an LLM" reads like the planner mostly goes away. It
does not. `SubmitPlan` is ~340 lines and the decomposition is about 60 of them;
everything else is admission control and materialization, and all of it stays.

> The CP planner stops being a **decomposer** and becomes an **admission
> controller and job materializer**: it decides *whether* the work may run,
> *where* it runs, *which models* it uses, and *what rows* represent it — and
> stops deciding *how* the work is split up.

**Unchanged.** The handler's org gating (`handler.go:44-59`: suspended orgs
rejected, free orgs pinned to `SharedFreeFleet`), the free-tier cold start
(`ensureFreeDaemon` + `wakeFleetHost`, `handler.go:71-74`), idempotency via
`PlanSubmission` and its `ON CONFLICT` path, the `OrgLimits` lookup, and the
single transaction that writes Job + Manifest + QueuedTask. Model, fleet, and
test-command policy still resolve on the CP and are stamped onto the spec — the
Control Plane still *chooses* the models even though it no longer *calls* them.

**Removed.** Planner model/key selection, `GetCredentialPlaintext`, the three
provider constructors, `p.Plan()`, and the usage aggregation
(`service.go:129-199`). `plan.Validate` stays in the code path but is trivially
satisfied by a one-worker plan, and `MaxWorkersPerJob` stops binding. In session
mode `SubmitPlan` makes **zero LLM calls and reads zero credentials.**

The shape of the change is small: a `SessionPlanner` implementing the existing
`Planner` interface, returning one `PlannedWorker{ID: "session", Task: req.Task}`
with no `File`/`Files` and no `DependsOn`. It needs no `Completer`, so it slots
in as a third branch of the existing selection at `service.go:124` and nothing
downstream in the transaction changes.

Four knock-on effects follow, and the first is a silent feature loss:

1. **Shared context would die by omission.** `ResolvedLearnings` is resolved on
   the CP — embedding plus org-scoped pgvector search (`service.go:96-114`) — and
   that must stay on the CP, because the daemon has no pgvector and no
   cross-tenant index. But its only consumer today is the planner it is handed
   to. Remove the CP planner and learnings are computed and dropped. They must be
   threaded onto the spec (`spec["learnings"]`) so the daemon's Architect
   receives them in its round-0 prompt.

2. **The manifest becomes a submission record, not a plan.** It is
   content-addressed (`contentHash`) and immutable, and `ver_hook.go:256-258`
   reads `summary`, `planner_model`, and `planner_provider` out of
   `manifest.Content` to build the execution record. In session mode the summary
   does not exist at submit time and cannot be patched in later without changing
   the manifest's hash. The manifest therefore keeps only submission facts (task,
   repo, ref, reference mode, chosen models, `mode: session`), and the execution
   record sources the plan summary and planner model/provider from the round-0
   `spec` event in the session log. That is a `pkg/ver` hook change, not only a
   planner change.

3. **Planner spend accounting inverts.** `Job.PlannerCostUSD`, `PlannerTokensIn`,
   and `PlannerTokensOut` are written at submit today. In session mode they are
   zero at submit and backfilled from the daemon's round reports. The
   `plannedOnOperatorKey` distinction becomes moot — planning always runs on the
   org's key inside the org's own daemon, so the org always pays. The Spend
   page's per-model breakdown reads `Job.Inputs["planner_model"]`, which stays
   accurate at submit because the CP still selects the model; only the cost lags.

4. **`plan.Summary` is unavailable at submit.** It feeds `SubmitResult.Summary`
   (rendered by the dashboard) and the `JobLearning` row. Semantic search is
   unaffected — the embedding is already computed from `req.Task`, not the
   summary (`service.go:108`) — and `UpsertJobLearning` is keyed on `job_id` and
   updates in place by design (`service.go:379`), so the daemon backfills the
   real summary once the Architect writes it. The API returns a placeholder until
   then.

**One regression that needs a deliberate decision.** Today an org with no
provider key fails *at submit*, with a precise message the dashboard maps to an
Integrations link — because the CP must read the key in order to plan. In session
mode nothing reads a key at submit, so that org receives a 202, a queued task,
and a failure minutes later inside the daemon. The fix is cheap and should ship
with the mode rather than after it: a credential **presence** check at submit
(the row exists; no decryption, no plaintext) that raises the same error string.

---

## 4. Trust boundary: **C2 does not move into the sandbox**

Decision: the Implementer's *model call* stays in the daemon process. Its *tool
effects* execute in the sandbox. The daemon becomes a **tool broker**.

```
  ┌─ daemon process ───────────────────────────┐        ┌─ sandbox (gVisor) ─┐
  │  Architect  ──spec──▶  Implementer         │        │                    │
  │   (LLM key)             (LLM key)          │        │   worktree         │
  │                            │ tool_use      │        │   toolchain        │
  │                            ▼               │        │   NO credentials   │
  │                     driver: validate ──────┼──exec──▶   NO network       │
  │                            ▲               │        │                    │
  │                            └── tool_result ┼────────┤                    │
  └────────────────────────────────────────────┘        └────────────────────┘
```

The existing invariant is preserved **verbatim**, because nothing about it
changes: the model runs where the key is, model-generated code runs where the
network is off, and the phase that has the network (dependency install) still
holds no secret. `isLLMKey` keeps doing its job unchanged.

But there is a real regression this design introduces, and it must be named
rather than waved past.

### 4.1 The exfiltration channel that opens

Today `testEnv` carries **every credential except the three LLM keys**
(`daemon.go:483`) — including `GIT_TOKEN`, which the same function later uses to
open the PR. That is safe today for one reason only: the sandbox runs *one fixed
command*, the one the user supplied, with the network off. A secret can be read
but not sent.

Under an agentic Implementer, the model chooses the commands **and the output
comes back to the daemon**, into the event log, into `ver` telemetry, and onto
the Control Plane. `loop.Event.Detail` already warns that "test output can carry
secrets." `echo $GIT_TOKEN` is now an exfiltration primitive with a network-free
egress path.

**Resolution: in session mode the sandbox gets no credentials at all.**

- The Implementer's shell runs with a credentials-free environment. Strictly
  stronger than today's Phase B.
- **Git is the daemon's job, not the agent's.** Fetch, commit, and push happen in
  the daemon process, outside the sandbox, using `GIT_TOKEN` — which therefore
  never enters a container. The agent gets no `git push` tool.
- **Installs stay brokered.** The Implementer gets an `install` tool that the
  driver executes as the existing Phase A: network on, no credentials. This is
  already a solved and documented case — `runTest` in `daemon.go:693` re-installs
  after the model edits a manifest, and explains precisely why that is safe
  ("the install is given nothing worth stealing").
- **Test-time credentials are opt-in and off by default**
  (`KIWI_SESSION_ALLOW_TEST_CREDS`). Some repositories' suites need a database
  password. Those orgs keep `file_loop` mode, or accept a documented downgrade.
  I want a decision on this rather than a default chosen quietly — it is the one
  place session mode is *less* capable than today's loop.
- **Output redaction** as defence in depth: the driver scrubs known credential
  values from every tool result before it reaches the event log. Cheap, and it
  should exist regardless.

### 4.2 What stays true about zero-knowledge

Unchanged, and slightly improved. No key moves toward the Control Plane. BYOC
still means the daemon holds the X25519 private key in the customer's cloud and
Kiwi cannot decrypt; managed still means Kiwi operates that machine and *can*,
so the zero-knowledge claim remains BYOC-only. Nothing here weakens it.

It gets *better* in one specific way. `pkg/planner/service.go:154` documents a
live containment gap: planning runs on the Control Plane with the org's decrypted
key, so "in BYOC that means Kiwi's network makes provider calls with a customer
credential — acceptable for managed, a containment gap for BYOC," and the comment
names moving planning into the daemon as the intended fix. Session mode does
exactly that: the Architect *is* the planner, and it runs in the daemon. In
session mode the CP makes no LLM call at all.

---

## 5. The Architect is persistent — and its context is reconstructed, not stored

Decision: yes, the reviewer becomes a task-long context. It is the same role as
the planner, not a separate one: **plan and review are one conversation**, which
is the whole point of the manual workflow being replicated.

Per round the Architect sees: the original user task, its own prior specs and
verdicts, a summary of what each round did, the **accumulated diff of the whole
task** (`git diff base..head` — not one file in isolation, which is today's
Critic's central weakness), the verification output, and the Implementer's
handoff note.

### 5.1 Where that conversation lives

**Not as a stored provider transcript.** The Architect's context is
*reconstructed from the event log* on every call (§7 schema).

This is a deliberate choice with three justifications:

1. **Provider portability.** Kiwi routes model → provider through
   `provider.ProviderOf` across three first-class providers. A persisted
   Anthropic message array is useless if a retry lands on a Gemini model, and
   `defaultProvider` selects the provider per task from the sealed bundle. A
   structured event log is provider-agnostic; the provider-native rendering is
   derived at call time.
2. **Crash recovery.** A reconstructed context resumes identically on a different
   daemon. A live provider session does not resume at all.
3. **Auditability.** `pkg/ver` signs a hash chain of task events. A reviewer whose
   entire context is derived from that chain is one whose decisions the execution
   record can actually explain.

The cost is honest and worth flagging: the model's own extended-thinking blocks
are not carried across rounds, so reasoning continuity is weaker than a true
persistent Opus conversation. If that proves to matter, the mitigation is additive — also
store the provider-native blob per round and use it opportunistically when the
same provider and model are still selected, falling back to reconstruction.

### 5.2 How the handoff differs from "rejection reason appended"

Today the Critic returns `{approved, reasons}` about one file's rewrite and the
reasons get concatenated onto the next Actor prompt inside the `buildOutput`
argument (`composeActorInput`, `loop.go:392`) — because the provider signature has
nowhere else to put it. The next Actor call has no memory that round 1 happened.

The Architect instead emits a **Spec**, which is the round's input rather than an
addendum to it:

```json
{
  "verdict": "revise",
  "rationale": "The retry wrapper is right, but it swallows context cancellation.",
  "objective": "Make the fetch path retry transient failures without masking cancellation.",
  "acceptance_criteria": [
    "ctx.Err() short-circuits before any retry sleep",
    "existing callers keep their current signature"
  ],
  "must_change":     ["internal/fetch/client.go"],
  "must_not_change": ["internal/fetch/client_test.go"],
  "hints":           ["the backoff helper already exists in internal/retry"],
  "open_questions":  [],
  "round_budget_usd": 1.50
}
```

`must_not_change` is worth calling out: it is where the anti-gaming rule from
`loop.go:240` moves. Today the loop refuses outright to edit a test while tests
are red. An Architect that can see the whole diff can express the same rule
per-round and more precisely — and unlike a static rule, it can permit "add tests
for the parser" when that is genuinely the task.

---

## 6. Cost control

Four mechanisms. The first is not optional — without it the design is unaffordable.

### 6.1 Cache-aware prompt layout

Fixed segment order, every Implementer call:

```
[ system prompt + tool definitions ]   ← stable for the whole session · 1h TTL
[ repo map ]                           ← stable for the whole session · 1h TTL
[ round spec ]                         ← stable for the round        · 5m TTL
[ transcript: tool_use / tool_result ] ← grows within the round      · rolling 5m breakpoint
```

The arithmetic, using this repo's own `PricingMap` (`pkg/provider/parse.go:15`;
`claude-sonnet-5` at $3/$15 per Mtok):

A 40-turn round whose transcript reaches ~80k tokens re-sends its prefix on every
turn — roughly 1.6M cumulative input tokens. Uncached that is **~$4.80 per
round**, so a 4-round session is ~$20 before the Architect says anything. With a
rolling cache breakpoint, ~90% of each turn's input is a cache read at 0.1×, and
the same round lands near **$0.70**.

Caching is therefore not a tuning knob here; it is the difference between a
viable product and a $20 task. Which has a direct implication:

> **`ModelCostUSD(model, inputTokens, outputTokens)` cannot express this.**
> `pkg/provider/parse.go:42` prices two token classes. Cached runs report four:
> uncached input, cache writes, cache reads, output. Until `Pricing` and
> `ModelCostUSD` gain cache-read and cache-write rates, every budget check and
> every Spend-page figure for a session run is wrong — over-counted by up to 10×,
> which will trip `MaxBudgetPerJob` in `LeaseNextTask` on runs that cost almost
> nothing.

That change is small and it blocks everything else in this section.

### 6.2 Compaction, deterministically triggered

*Within* a round: when the transcript exceeds `CompactAt` (default 100k tokens),
the driver replaces the middle — oldest tool results first — with a
model-generated summary, and keeps verbatim: the spec, the last K tool results
(default 8), and the current diff. The summary is emitted as a `compaction` event
so it is auditable and the round is replayable.

*Across* rounds: no compaction is needed, because rounds start fresh. This is a
second dividend of §2 — the design's most expensive context-management problem
simply does not arise.

The Architect compacts by construction: it consumes round summaries and diffs,
never tool transcripts. Its context grows by a few thousand tokens per round.

### 6.3 Hierarchical budgets

`MaxBudgetUSD` becomes three nested caps, checked before every tool call, every
round, and every Architect call:

| Cap | Default | Enforced by |
|---|---|---|
| `round_budget_usd` | $1.50 | driver, between tool calls |
| `session_budget_usd` | $5.00 | driver, at round boundaries |
| `MaxBudgetPerJob` | existing org limit | **already** enforced in `LeaseNextTask` |

The third already exists and already fires — it is the backstop that survives a
buggy driver. Note that today's default of **$0.50 is roughly 10× too low** for a
session; that is a pricing decision, not a config default (see risks).

Spend is reported to the Control Plane at every round end, not only at
completion, so a runaway is visible while it runs and can be stopped by lease
revocation — a path that already works (`daemon.go:300`: lease lost → `taskCancel()`).

### 6.4 Tiering

Rounds are few and tool turns are many, so cost concentrates in the cheap tier.
A representative session: ~4 Architect calls at Opus rates (~$0.80 total) and 3–4
Implementer rounds at cached Sonnet rates (~$2.10). Total **$2–4**, versus
**$15–25** with the same design and no caching.

---

## 7. Persistence and crash recovery

### 7.1 The claim

**The round is the atomic, restartable unit of work.** Because the Implementer is
stateless across rounds (§2), a round has exactly the property the lease queue
requires: it can be thrown away and redone from durable inputs. The task-long
conversation that *is* stateful — the Architect's — is reconstructed from a log,
not held in memory.

So the lease queue's assumption is not violated; it is satisfied at a coarser
granularity than today.

### 7.2 Where state lives

Two places, both already durable:

1. **git.** The job branch `kiwi/<jobID>` is pushed at the end of every round.
   The diff is the work product and it survives the daemon by construction. This
   is not new infrastructure — `GetJobWorktree` and `publishResult` already treat
   the job branch as the shared substrate.
2. **Postgres**, as an append-only event log.

### 7.3 Schema — migration `0021_agent_sessions`

Following the convention and the lesson of `0020`: guarded DDL, because
`queued_tasks` itself exists only via `AutoMigrate` and the `migrate` role may run
against a database no serving process has touched.

```sql
-- migrations/0021_agent_sessions.up.sql
CREATE TABLE IF NOT EXISTS agent_sessions (
    id                 TEXT PRIMARY KEY,
    org_id             TEXT NOT NULL,
    job_id             TEXT NOT NULL,
    task_id            TEXT NOT NULL,              -- the queued_tasks row that leases it
    repo_url           TEXT NOT NULL,
    base_ref           TEXT NOT NULL,
    branch             TEXT NOT NULL,
    base_sha           TEXT,                       -- immutable session start point
    head_sha           TEXT,                       -- last committed round
    phase              TEXT NOT NULL,              -- planning|implementing|verifying|reviewing|delivering|done
    round              INT  NOT NULL DEFAULT 0,
    round_attempts     INT  NOT NULL DEFAULT 0,    -- attempts at the CURRENT round
    max_rounds         INT  NOT NULL DEFAULT 4,
    spec_current       JSONB,                      -- the Architect's spec for `round`
    architect_model    TEXT NOT NULL,
    worker_model       TEXT NOT NULL,
    cost_usd           DOUBLE PRECISION NOT NULL DEFAULT 0,
    tokens_in          BIGINT NOT NULL DEFAULT 0,
    tokens_out         BIGINT NOT NULL DEFAULT 0,
    status             TEXT NOT NULL,              -- RUNNING|SUCCEEDED|FAILED|CANCELLED
    deadline_at        TIMESTAMPTZ,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_agent_sessions_org_job ON agent_sessions (org_id, job_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_agent_sessions_task ON agent_sessions (task_id);

CREATE TABLE IF NOT EXISTS agent_session_events (
    id           BIGSERIAL PRIMARY KEY,
    session_id   TEXT NOT NULL REFERENCES agent_sessions(id) ON DELETE CASCADE,
    org_id       TEXT NOT NULL,
    round        INT  NOT NULL,
    seq          INT  NOT NULL,                    -- monotonic within (session, round)
    kind         TEXT NOT NULL,
    payload      JSONB NOT NULL,
    digest       TEXT,                             -- sha256 of payload, for pkg/ver chaining
    cost_usd     DOUBLE PRECISION NOT NULL DEFAULT 0,
    tokens_in    BIGINT NOT NULL DEFAULT 0,
    tokens_out   BIGINT NOT NULL DEFAULT 0,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_ase_seq ON agent_session_events (session_id, round, seq);
```

Event `kind` vocabulary — an extension of the existing `initial_test | actor |
critic | test` phase vocabulary rather than a replacement, so `ver.TaskEvent`
keeps working:

| kind | payload | written by |
|---|---|---|
| `spec` | the Architect's Spec object | Architect |
| `round_start` | base sha, spec digest, budget grant | driver |
| `tool_call` | tool name, redacted args, **digest** of result, exit code, duration | driver |
| `patch` | files touched, insertions/deletions, tree sha | driver |
| `install` | command, source, outcome | driver |
| `verify` | test command, pass/fail, output tail | driver |
| `handoff` | the Implementer's note | Implementer |
| `compaction` | the summary that replaced transcript middle | driver |
| `review` | verdict, rationale | Architect |
| `round_end` | head sha, cost, tokens, outcome | driver |
| `session_end` | terminal status, PR URL | driver |

Note `tool_call` stores a **digest** of the result, not the result. Tool output is
large and, per `loop.Event.Detail`'s own warning, may carry secrets. Full output
lives in the round transcript, which is in-memory and discarded; the log keeps
what is needed to audit and to resume, not to replay verbatim.

### 7.4 What resuming looks like

A daemon leases task `T` and finds `spec.mode == "session"`:

1. `SELECT ... FROM agent_sessions WHERE task_id = T`. Absent → new session,
   round 0, Architect writes the first spec.
2. Present → **the in-progress round is discarded, not resumed.** Reset the
   worktree to `head_sha` (`git fetch && git reset --hard <head_sha>`), increment
   `round_attempts`, and re-run round `round` from `spec_current`.
3. Commits carry a `Kiwi-Round: <n>` trailer, so a round that was pushed before
   the crash but not recorded is detectable and idempotently skipped rather than
   applied twice.
4. Rails on resume: `round_attempts >= 2` fails the session (a round that kills its
   daemon twice is a poison pill), and the existing `MaxLeaseAttempts = 5`
   dead-letter in `RequeueExpiredLeases` remains the outer backstop.

Discarding partial rounds rather than resuming mid-transcript is the point of §2:
the loss is bounded by one round's budget, and the alternative — persisting a
half-finished provider transcript and its pending tool call — is exactly the
fragile, provider-specific state this design refuses to own.

**Lease mechanics that must change.** Renewal is time-based and already fine
(4-minute ticker, `RenewLease` extends TTL). Two things do not survive contact
with a long session: the default `TimeoutSeconds` of 1800 becomes a *round*
deadline with a separate, longer session deadline; and the driver should check
for lease revocation **between tool calls** rather than only on the renewal
ticker, so a user-requested cancel stops the work in seconds rather than minutes.

---

## 8. Safety rails

Each of today's rails has a successor. None is dropped.

| Today | Session mode | Default |
|---|---|---|
| `MaxSteps` 6 | `MaxRounds` | 4 |
| — | `MaxToolCallsPerRound` | 60 |
| — | `MaxConsecutiveToolErrors` | 5 |
| `MaxBudgetUSD` $0.50 flat | round / session / job hierarchy (§6.3) | $1.50 / $5 / org limit |
| `dupOutputHalt` 3 | no-progress detector, two scales (below) | — |
| `rejectionHalt` 3 | `MaxRejections` on Architect verdicts | 3 |
| — | Architect-loop detector (below) | — |
| `TimeoutSeconds` 1800 | `round_deadline` + `session_deadline` | 15 min / 90 min |
| abuse: timeout && steps < 2 | abuse: timeout && rounds_completed < 1 | — |

**No-progress detection, generalized.** Today's rail hashes test output. A session
has two scales:

- *Within a round*: an identical `(command, output)` pair three times injects an
  explicit warning into the transcript; five times kills the round. Injecting
  before killing is deliberate — a model told "you have run this exact command
  three times with the same result" frequently recovers, and today's loop never
  gets to say so.
- *Across rounds*: hash `(head tree sha, verification output)` at round end. Two
  consecutive identical hashes means the session is spinning and halts. This
  subsumes the `errNoChanges` case that `delivery.go` already handles well — an
  empty diff at round end is a no-progress round, not a success.

**Architect-loop detection** is new and necessary. An open-ended reviewer can
issue the same rejection forever. If consecutive specs have the same
`must_change` set and semantically equivalent acceptance criteria, halt: the
reviewer is looping, and today's `rejectionHalt` comment already documents this
exact failure mode from a real run (the `examples/advanced.rs` case).

**Tool-level rails**, all new because the surface is new: writes confined to the
worktree (generalizing the existing `filepath.IsLocal` check), no writes under
`.git` (the daemon owns git), no network tool, per-round wall-clock enforced
through the sandbox context — the reasoning in `daemon.go:459` about deriving the
sandbox context from the capped task context applies unchanged, per round.

**What actually stops a runaway**, in order of speed: the round budget (seconds),
the round deadline (minutes), the session budget, the session deadline, the
org-level `MaxBudgetPerJob` in `LeaseNextTask`, and finally lease revocation from
the Control Plane, which the daemon already honours.

---

## 9. Model assignment and BYOC

It fits, and it mirrors the manual split exactly.

Both roles run in the daemon on the customer's key(s), built from the same sealed
bundle. `defaultProvider(creds, model)` already does the routing — model →
`ProviderOf` → `CredentialNameFor` → key — so this is two calls where there is
one today. `WorkerSpec` gains `architect_model` and `worker_model`;
`PlannedWorker` and `PlanRequest` gain the same.

Two consequences worth stating:

- An org with one provider key splits by *model within that provider* (Opus
  Architect, Sonnet Implementer). An org with several can split across providers —
  Opus Architect, Gemini Flash Implementer — which is a genuinely nice BYOC
  property and comes for free from `ProviderOf`.
- If an explicitly requested `architect_model` has no key in the bundle, **fail
  loudly**, matching today's precise message ("no API key configured for the %s
  provider that model %q needs"). Silently downgrading the reviewer to the cheap
  tier would degrade quality invisibly, which is the one failure mode a reviewer
  must not have.

And, per §4.2: in session mode the Control Plane makes no LLM call, so planner
spend disappears from the CP and the BYOC containment gap documented in
`planner/service.go` closes.

---

## 10. Migration: opt-in mode, alongside `pkg/loop`

**Do not replace `pkg/loop/loop.go`.** Three reasons:

1. It is the only live execution path, and its behaviour is pinned by six test
   files encoding hard-won specifics — additive tasks, file creation, JSON repair,
   rejection halting, event emission. Those are not incidental; each one is a bug
   that reached a user.
2. Session mode depends on a primitive the codebase does not have at all
   (tool-calling), across three providers.
3. The free tier's economics are built around short bounded loops on metered
   agent-minutes. Long sessions need a pricing answer first (§11).

Selection seam: `WorkerSpec.Mode` — `"file_loop"` (default) or `"session"` — set
by the planner, gated by an org limit and an env flag. `executeTask` branches once.

### Phases, cheapest and lowest-risk first

**Phase 0 — the two new primitives. No Postgres, no CP, no lease changes.**
- `provider.ToolRunner`: a tool-calling interface plus an Anthropic implementation.
  One provider only; Gemini and OpenAI follow the same seam later.
- `sandbox.Session`: `docker create`/`start` + `docker exec` per tool call, same
  image inference, same gVisor runtime, same mounts, `--network none`. Replaces
  40 container starts with one.
- Prove it with a throwaway `cmd/` harness against `demo_project`.
- *This is the slice that de-risks the design.* If a tool loop cannot drive the
  existing image inference and gVisor sandbox reliably, nothing downstream matters.

**Phase 1 — the workflow, in memory.** `pkg/session` with the full round
structure: Architect spec → Implementer round → verify → Architect review →
repeat, bounded by in-memory `MaxRounds` and budgets, inside one leased task,
reusing today's task timeout and `publishResult` for the PR. Crash recovery is
today's behaviour: the lease expires and the task restarts from scratch, which is
acceptable while sessions are short and bounded.

**This is the smallest slice that proves the design** — Phases 0+1 together
replicate the manual workflow end to end and produce a PR. Gate the rest on an
eval: run session mode and `file_loop` mode over the same ~20 tasks and compare
PR-opened rate, human-accept rate, cost, and wall clock. If session mode does not
win there, the durability work is wasted.

**Phase 2 — durability.** Migration `0021`, the event log, round checkpointing,
resume-from-round, the `Kiwi-Round` trailer. Now sessions can be long and
crash-safe.

**Phase 3 — cost.** Cache-aware prompt layout; **`Pricing`/`ModelCostUSD` cache
token classes** (§6.1 — this one is a prerequisite for trustworthy budgets, so it
may want to move earlier); compaction.

**Phase 4 — collapse the plan** (§3.1 is the checklist). `SessionPlanner` emits
one worker with no CP LLM call; thread `ResolvedLearnings` onto the spec so
shared context survives; move plan summary and planner model/provider in
`ver_hook` from the manifest to the round-0 `spec` event; backfill planner spend
and the `JobLearning` summary from daemon reports; add the submit-time credential
presence check. Dashboard shows rounds instead of workers.

**Phase 5 — optional.** Provider parity (Gemini, OpenAI tool-calling), then
disjoint-milestone parallelism if it is still wanted, which it may not be.

---

## 11. Risks and judgment calls to sanity-check

1. **Fresh-per-round C2 is the load-bearing call.** Everything about crash
   recovery follows from it. If re-exploration turns out to be expensive or lossy
   on large repositories, the fallback is a richer handoff note and a persisted
   repo map — not statefulness, which would drag the durability story with it.
   Worth measuring in Phase 1 before Phase 2 is built.

2. **Session mode is less capable than today's loop for repos whose tests need
   secrets** (§4.1). I chose no credentials in the sandbox by default. That is a
   real capability removal, and it is the decision I most want challenged.

3. **A persistent container is a longer-lived attack surface** than today's
   `docker run --rm` per command — on the shared free-fleet host especially, where
   CLAUDE.md's `DOCKER-USER` egress note applies and a `--network host` container
   would bypass it entirely. It also holds 512m and a CPU for the session's life
   rather than a test run's.

4. **Agent-minute economics.** Metering runs from `StartedAt` for the whole leased
   task. A 90-minute session bills 90 agent-minutes; a free-tier org's monthly
   allowance could go in one or two tasks. The default budgets in §6 are ~10× the
   current `$0.50`. This is a pricing decision that gates the free tier, not a
   config default.

5. **Reconstructed Architect context loses extended-thinking continuity** (§5.1).
   This is a genuine fidelity loss against the manual Opus workflow, traded for
   provider portability and crash recovery. Mitigation exists and is additive, but
   it is a trade, not a free win.

6. **Throughput and UX optics.** Killing per-file parallel workers makes a job
   look like one long unit instead of several concurrent ones. Wall-clock per task
   may go *up* even as quality and cost improve. The dashboard's worker-oriented
   view needs to become round-oriented, and someone should decide whether the
   product story survives "fewer, slower, better."

7. **Budget accounting is wrong until §6.1 lands.** `ModelCostUSD` prices two
   token classes; cached calls report four. Running session mode before that fix
   means over-counted spend tripping `MaxBudgetPerJob` on cheap runs. Consider
   pulling that specific change into Phase 0.

8. **Submit stops being the place bad configuration is caught** (§3.1). Moving
   planning into the daemon means a missing provider key, and any other fault the
   CP currently discovers by trying to plan, surfaces minutes later in a queued
   task instead of immediately in the API response. The presence check covers the
   known case; the general shift — submit gets cheaper and later-failing — is
   worth agreeing to on purpose.

9. **`MaxRounds = 4` and `MaxToolCallsPerRound = 60` are guesses.** They are the
   two numbers that most directly determine both quality and cost, and neither has
   any evidence behind it yet. Phase 1's eval should set them, not the author of
   this document.
