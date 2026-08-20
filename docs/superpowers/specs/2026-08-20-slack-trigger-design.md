# Kiwi Slack Trigger — Design Spec

**Target repo:** `RunKiwi/kiwi` (this repo). New Control-Plane-owned integration, licensed BSL 1.1 under `ee/`, same as `ee/githubapp`.

**Status:** Design approved via brainstorming session, 2026-08-20. Ready for `writing-plans`.

**Implementer note:** where a decision below is closed, it says so and gives the reason — don't relitigate it. Where a numeric threshold or exact wording is marked as a tuning knob, that's deliberate: pick a reasonable default and note it in the PR description rather than treating it as a blocker. If anything here contradicts what you find in the actual code, the code wins — flag the discrepancy rather than silently picking one.

---

## 1. Context

Customer requirement: trigger Kiwi tasks from Slack (messages, threads) and from GitHub (issues, PRs). A codebase survey found GitHub already has ~90% of the needed plumbing live — `ee/orchestrator/pr_comment_trigger.go` already turns a `@runkiwi <instruction>` PR comment into a task (HMAC-verified webhook, installation-token auth, write-access checks, dedupe, reply-in-thread primitives) — while Slack has **no inbound integration at all** (only an outbound `SLACK_WEBHOOK_URL` credential that posts monitor verdicts).

Given that asymmetry, this spec builds **Slack first**. Slack forces solving the hard version of a shared problem — "given a bare trigger with no repo/test-cmd signal, what task should this even be?" — before GitHub-issue-triggering reuses the same answer for its easier case (a GitHub webhook payload is already self-describing: `repository.owner.login` + `repository.name` come for free).

GitHub-issue-triggering, general non-PR `output_type` support, Jira/Linear as inference signals, and a plan-mode approval gate are explicitly **out of scope** here — see §9.

## 2. Decisions (already made — do not re-ask)

| Question | Decision | Why |
|---|---|---|
| Trigger mechanism | `@mention` the bot in a channel or thread | Natural for both fresh messages and thread replies (continuations); a slash command can't reply inside an existing thread. |
| Context source | Not just the tagged message — walk back prior history | A bare `@runkiwi fix this bug` is meaningless without what "this bug" refers to; the referent is almost always in the surrounding conversation. |
| Repo resolution | Per-channel binding → inline override → LLM inference over the org's GitHub repo list → disambiguate | No org-level default repo exists anywhere in Kiwi today; a Slack message has zero inherent repo signal (unlike GitHub, where the webhook payload names the repo). |
| Repo-inference signals | GitHub-installed repo list only. Jira/Linear **parked**. | Validate the cheap version (LLM over repo names/descriptions, using infra that already exists) before building two more OAuth integrations to test a hypothesis that's still unproven. |
| `test_cmd` resolution | Static repo-convention detection first, Architect-inferred fallback | Nobody types a test command into Slack. Static detection keeps the guard external (CLAUDE.md: "the test command is a guard... not the definition of done") for the common case; Architect inference only covers repos with no detectable convention. |
| Slack↔org identity | Self-serve OAuth install ("Add to Slack") | Mirrors `GitHubInstallation` — no manual credential handling by the customer. |
| Trigger permission | Anyone who can post in a bound channel | Same trust model as a team already using a dedicated Slack channel for a repo; no separate allowlist to build and keep in sync. |
| Thread continuation | A reply in an actioned thread continues by default; Architect classifies continue/fork/new/ambiguous | Mirrors GitHub's `SubmitContinuation` pattern; "fork" and "new" cover the real cases where a reply isn't actually about the same fix. |
| Status UX | React 👀 on trigger; one status message per task, **edited in place** through created→running→done; emoji swapped at each transition | Avoids spamming the channel with a new message per status change; still gives an at-a-glance signal via the emoji on the original message. |
| Non-PR completion | In scope, narrowly: "investigation-only" success state + "create a GitHub issue" action | The flagship "@runkiwi investigate this bug" example produces nothing useful under a PR-only model. Full `output_type` generalization stays parked (§9) — this is two bounded additions, not that redesign. |

## 3. Architecture

New package `ee/slackapp`, structured like `ee/githubapp`: an OAuth install flow plus a thin Slack API client (post message, edit message, add/remove reaction, fetch channel/thread history, post interactive Block Kit buttons). Two new HTTP endpoints, mounted unauthenticated on the root mux next to the existing GitHub/Linear/Stripe webhooks (`server.go:513-518`) — the payload's own signature is the auth, same as those three:

- `POST /api/v1/webhooks/slack/events` — Slack Events API (`app_mention`, `message` in a tracked thread) and interactivity payloads (`block_actions`, for the continue/fork/new buttons). Verified via `X-Slack-Signature` + `X-Slack-Request-Timestamp`, HMAC-SHA256 over `v0:{timestamp}:{body}`, app-wide `SLACK_SIGNING_SECRET` (fail-closed if unset, same posture as `GITHUB_WEBHOOK_SECRET`).
- `GET /api/v1/integrations/slack/oauth/callback` — completes the "Add to Slack" install, authenticated via a `state` param tied to the initiating dashboard session.

Dashboard additions: an "Add to Slack" button on the Integrations page, and a new Channel Bindings page (list/add/remove `SlackChannelBinding` rows for the connected workspace).

## 4. Data model

Three new tables, all `org_id`-scoped per the standing multi-tenant rule:

```
SlackInstallation
  ID, OrgID, TeamID, TeamName, BotTokenSealed, InstalledByUserID, CreatedAt

SlackChannelBinding
  ID, OrgID, TeamID, ChannelID, RepoURL, DefaultTestCmd (nullable), DefaultRef (nullable), CreatedBy, CreatedAt

SlackTriggeredTask
  ID, OrgID, TeamID, ChannelID, ThreadTS, ParentTaskID (nullable), QueuedTaskID,
  StatusMessageTS, LastStatus, InvestigationOnly (bool), CreatedAt, UpdatedAt
```

`SlackTriggeredTask` is **not** one row per thread — a thread can accumulate multiple tasks over time (fork, new), so continuation/classification logic always reads the *latest* row for a given `ThreadTS` as "current context." `BotTokenSealed` follows the existing credential-sealing convention (`pkg/crypto`) — never stored plaintext, and, like `SLACK_WEBHOOK_URL` today, never exposed to the sandbox's test-command environment (`pkg/daemon/session_run.go`'s credential-exclusion list gets a new entry).

Add via a new numbered migration per the standing convention (`migrations/*.up.sql`, `RunMigrations`).

## 5. Repo resolution

In priority order, first match wins:

1. **Inline override** — an explicit repo mentioned in the trigger message (e.g. `repo:org/name` or a bare `org/name` token). Implementer's call on exact syntax; document whatever's chosen in the README.
2. **Channel binding** — `SlackChannelBinding` row for `(TeamID, ChannelID)`.
3. **LLM inference** — call the org's `ListRepositories()` (`ee/githubapp/client.go`), pass the repo names + descriptions + the assembled task context (§6) to a small LLM call, ask it to pick the most likely target or report "ambiguous." Confidence threshold is a tuning knob — start conservative (only auto-pick on a clear single winner) and widen based on real usage rather than guessing a number now.
4. **Disambiguate** — if inference reports ambiguous or the org has no GitHub installation at all, reply in-thread naming the top candidates (or asking the user to connect GitHub / bind the channel) and stop. No task is created.

## 6. Context assembly

On any trigger:

1. If the mention is a thread reply, pull the entire thread from its root. If it's a fresh top-level message, pull the last ~10 channel messages preceding it.
2. Ask the LLM (cheap call): "is this enough context to act on the instruction, or do you need more?" If insufficient, escalate the lookback (e.g. to 50, or the full channel history since the thread started) before giving up.
3. If still insufficient after escalation, reply in-thread asking the user to clarify rather than guessing.
4. Compose the task description from the gathered messages + the trigger instruction. This assembly step should be a standalone, generically-named function (not Slack-specific in its signature) so GitHub-issue-triggering can reuse the same "raw trigger → enriched instruction" shape later — see §9.

## 7. `test_cmd` resolution

At plan time, before submission:

1. **Static detection** — inspect the target repo for known conventions: `go.mod` → `go test ./...`, `package.json` with a `scripts.test` entry → `npm test`, a recognizable CI workflow file (`.github/workflows/*.yml`) → extract its test-invocation step if unambiguous. Deterministic, no model call.
2. **Architect fallback** — if nothing conventional is found, the Architect infers a verification approach at runtime, the same judgment call an interactive Claude Code session already makes. This is a weaker guard (self-graded) and only the fallback path, not the default — see the tension named in §2.

This applies to `PlanRequest.TestCmd` generally (`ee/planner/planner.go`), not just Slack-originated submissions — any future caller that omits `test_cmd` benefits, though this spec only wires it into the Slack path.

## 8. Task lifecycle & Slack UX

**First trigger:**

1. Verify signature → resolve org via `team_id` → `SlackInstallation`.
2. React `:eyes:` on the trigger message immediately (synchronous ack).
3. Resolve repo (§5) — if disambiguation is needed, stop here, no task yet.
4. Assemble context (§6), resolve `test_cmd` (§7).
5. Architect classifies the task **during planning, before any work starts**: normal (code change expected) or investigation-only. This classification is recorded up front and is what makes "no diff produced" a valid success state later — it is never inferred after the fact from the absence of a diff. (CLAUDE.md already documents the failure mode this guards against: an earlier version treated "nothing changed" as success and silently no-op'd real requests. A code-fixing task that produces no diff is still a failure, full stop — only a task explicitly classified investigation-only at the start gets the no-PR success path.)
6. Submit via `ee/planner.SubmitPlan`. Investigation-only and normal tasks both run through the same daemon/session execution — same sandbox, same two-phase invariant (untrusted model commands stay sandboxed either way); only the *tail end* differs (§8.1).
7. Post the status reply in-thread (task link), create the `SlackTriggeredTask` row (`StatusMessageTS` = this reply's timestamp), swap emoji to `:hourglass_flowing_sand:`.
8. On terminal state, **edit that same status message** with the result and swap emoji to `:white_check_mark:` (success) or `:x:` (failed). Emoji names above are defaults, not contractual — fine to adjust during implementation.

**8.1 Terminal state, normal task:** PR link + summary in the edited status message, same as any other Kiwi task.

**8.2 Terminal state, investigation-only task:** no PR attempted. Findings become the job's summary (same field `getJob`/`store.Job` already expose for every task) and the final edited status message content, truncated with a "view full report" link to the dashboard task page if long. If the instruction explicitly asked for a GitHub issue ("...and create a GitHub issue"), file one using the existing `installationToken` + create-issue primitives (`ee/orchestrator`'s comment-trigger code already has both), only when explicitly requested — not a default behavior for every investigation.

**Thread reply (existing `SlackTriggeredTask` for this `ThreadTS`):**

1. Send the latest task's state/summary + the new message to the Architect, ask it to classify: continue / fork / new / ambiguous.
2. **Continue** — `SubmitContinuation` (exists, `ee/planner/continuation.go`), same as GitHub's comment-trigger pattern.
3. **Fork** — new capability, small addition to `ee/planner`: a `parent_task_id` on `PlanRequest` that resolves to starting from that task's current branch instead of `ref` on main. Not a redesign of the planner, just a second way to pick a starting point.
4. **New** — normal `SubmitPlan`, unrelated task, still logged as a new `SlackTriggeredTask` row against the same `ThreadTS` (§4 — threads aren't 1:1 with tasks).
5. **Ambiguous** — post an interactive Block Kit message (buttons: New / Fork / Continue) in-thread and wait for the `block_actions` interactivity payload before proceeding with whichever the user picks.

## 9. Explicitly out of scope (parked, tracked in Obsidian `RunKiwi Open Actions.md`)

- **GitHub-issue-triggering** — next spec. Extends the live comment-trigger pipeline (`pr_comment.go` currently drops non-PR `issue_comment` events and doesn't handle standalone `issues` events at all); reuses this spec's context-assembly shape (§6).
- **General non-PR `output_type`** — this spec only adds the two narrow slices in §8.2. A full task-model concept for arbitrary output shapes across every trigger surface is a bigger, separate change to `pkg/session`.
- **Jira/Linear as repo-inference signals** — validate LLM-over-GitHub-repo-list first.
- **Plan-mode with human approval gate** — a control-flow change (pause before acting), orthogonal to output shape. Shares building blocks with §8.2's investigation-report mechanism and this spec's interactive-buttons mechanism (§8, step 5), so building it later gets a head start here. Needs its own brainstorm, particularly around what "default" scopes to (see the Obsidian TODO for the open question).

## 10. Testing

- Pure-function extraction for signature verification and event/payload parsing (table-driven, no DB/network), same pattern as `pr_comment.go`'s existing tests.
- A fake Slack API client for OAuth token exchange, channel/thread history fetch, message post/edit, and reaction add/remove — mirrors whatever fake exists for `ee/githubapp` in orchestrator tests.
- Cover the classification safeguard explicitly: a test asserting that a *normal* task producing no diff is still a failure, and only an upfront-classified investigation-only task gets the no-PR success path.
