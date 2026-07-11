# Kiwi Console — Product & UX Design Document

**Status:** Draft for review
**Date:** 2026-07-11
**Owner:** Product + Frontend
**Companion:** System RFC — *Distributed Agentic Execution Platform* (control-plane/sandbox architecture, data model, observability). This document is the **customer-facing UI** counterpart.

> **How this doc was produced.** A proposer enumerated every plausible console surface (47); a devil's-advocate pruned it against *customer value and product coherence* (not backend readiness); this document synthesizes the survivors into a coherent product. The v1/roadmap/cut decisions and their rationale are recorded in §11.

---

## 1. Overview & Goals

Kiwi runs LLM **Actor–Critic agent workflows** inside isolated sandboxes. The Console is the product surface where customers **submit** agent jobs, **watch** them execute step-by-step in real time, **act** on them (pause/cancel/retry/resume/approve), and **govern** cost and access across a team.

The console is not a thin status page — the differentiator is **legibility**: a customer can see exactly what each agent (master + workers) planned, what the Actor changed, what the Critic said, which tools ran, what it cost, and what changed in their code — live, and after the fact.

### Goals
- **G1 — Make agent runs legible.** The per-phase Actor→Critic timeline with live streaming is the flagship experience.
- **G2 — Build trust to act on agent output.** Diff review, manifest/audit transparency, clear failures, and cost visibility so customers trust (or reject) what the agent did.
- **G3 — Self-service the whole loop.** Submit → monitor → act → govern, without touching a CLI.
- **G4 — Governance for teams.** Cost/budget visibility, quotas, roles, keys, and audit for org admins.
- **G5 — Feel like a product, not a demo.** Onboarding, empty states, notifications, help, error clarity, deep links, responsive, accessible.

### Non-goals (v1)
- In-browser workflow/manifest authoring (CLI-first; see Roadmap).
- Operator/fleet tooling and re-implemented metrics dashboards (delegated to Grafana/OTel via deep-links).
- Human-in-the-loop approval UX beyond the specified hook (Roadmap).

---

## 2. Personas & Jobs-To-Be-Done

| Persona | Primary goal | Key surfaces |
|---|---|---|
| **Developer** (submits/monitors jobs) | "Run an agent on my codebase and trust the result." | Submit modal, Run detail (timeline/logs/diff), notifications, personal keys |
| **Org Admin** (governs the team) | "Control who can run what, and what it costs." | Users & roles, API keys, provider config, limits, budget/usage, audit |
| **Operator / Viewer** (oversight) | "Is everything healthy and within budget?" | Jobs table, usage dashboard, audit; deep-links to Grafana |

**JTBD spine (all personas):** *Submit → watch the Actor/Critic timeline live with cost/diff/manifest trust surfaces → act (pause/cancel/retry/resume/reconnect) → govern (users/keys/limits/audit).*

---

## 3. Design Principles
1. **Live by default.** State streams; the user never guesses whether something is progressing.
2. **Truthful numbers.** Cost and budget always reflect real limits — never a hardcoded placeholder.
3. **Trust surfaces first-class.** Never ask a user to accept agent-written code blind; show the diff, the manifest, the cost.
4. **One way to do a thing.** No duplicate list views or duplicate live feeds.
5. **Legible failure.** Every failed job answers "why" and offers the next action.
6. **Progressive disclosure.** A run detail reveals depth (topology, checkpoints, manifest) on demand, not all at once.
7. **Accessible & responsive from day one**, not retrofitted.

---

## 4. Information Architecture

```
Kiwi Console
├─ Login (API key; SSO on roadmap)
├─ App shell: left nav · top bar (org/user badge, notifications, help, settings)
│
├─ Jobs                     ← default landing
│   ├─ Jobs table (filter · search · paginate)     [primary list]
│   └─ Submit Job (modal, from here)
│
├─ Run Detail (/jobs/:id)   ← flagship
│   ├─ Header (goal · status · cost · controls · share)
│   ├─ Timeline  (Actor→Critic, live)   ← default tab
│   ├─ Agents    (master/worker topology)
│   ├─ Logs      (live stream)
│   ├─ Changes   (workspace diff)
│   ├─ Manifest  (immutable config/audit)
│   ├─ Cost      (per-job cost & tokens)
│   └─ Checkpoints (list · resume)
│
├─ Usage & Budget           ← org spend vs limit
│
├─ Admin  (admins only)
│   ├─ Users & Roles
│   ├─ API Keys
│   ├─ Provider Config (LLM keys/models)
│   ├─ Limits (quotas/budget)
│   └─ Audit Log
│
└─ Settings
    ├─ Connection (daemon URL + token)
    ├─ Preferences (theme, density, timezone, live/poll)
    └─ My API Keys
```

Navigation is role-aware: Admin is hidden for non-admins; Usage is visible to admins/operators.

---

## 5. Core User Journeys

### 5.1 First run (onboarding)
1. Login (API key) → **empty Jobs table** with a first-run panel: "Submit your first job" + a link to docs and a one-click **example job**.
2. Submit modal pre-filled with a sample workflow.
3. On submit → land directly on **Run Detail → Timeline** (live), so the first thing a new user sees is the product's core value in motion.

### 5.2 The flagship journey — watch an agent work
On Run Detail → Timeline, the user watches events stream in as semantic cards, grouped per agent and per loop iteration:

```
▸ initial_test        FAIL   (0.4s)     "panic: divide by zero"
▼ Iteration 1
   ▸ actor            proposed (3.1s)   +tokens 1,240/320   $0.02   [view diff]
   ▸ critic           approved (1.2s)   "safe minimal fix"  +tokens 900/40
   ▸ tool: go test    pass   (0.5s)
✓ SUCCESS   total $0.14   2 iterations   47s
```

Each card is expandable (full reasoning summary, tool I/O, token/cost). A **cost/token counter** in the header ticks up live. A **[view diff]** affordance on any actor step deep-links to Changes. If a worker is involved, cards are tagged with the worker id and filterable via the Agents tab.

### 5.3 Act on a run
From the Run Detail header (and quick-actions on the jobs table): **Pause**, **Resume**, **Cancel**, and — on terminal states — **Retry** and **Re-run (replay manifest)**. Destructive/costly actions (Cancel) confirm first.

### 5.4 The secret-tunnel reconnect moment (distinctive UX)
When a job pauses because it needs a credential from the developer's just-in-time tunnel, Run Detail shows a prominent **PAUSED — awaiting your credential** banner: which secret, why, and a **Reconnect tunnel** call-to-action with copy-paste CLI. Resuming is automatic on reconnect; the banner clears live.

### 5.5 Govern (admin)
Admin invites teammates, sets roles, issues/revokes API keys, configures the LLM provider + per-role models, sets org **limits** (concurrency, per-job/monthly budget, timeout, disk), and reviews the **audit log**. The **budget bar** everywhere reflects the real monthly limit.

---

## 6. Screen-by-Screen Specification

Every screen specifies its **states**: `loading`, `empty`, `error`, and (where applicable) `live`.

### 6.1 Login & App Shell
- **Login:** full-screen; API key + daemon URL; validates via identity check; clear error on bad key ("Invalid or expired API key"). SSO button placeholder (roadmap, hidden until enabled).
- **Top bar:** org + user badge (name/email/role), **notifications** bell, **help** (?), settings gear, logout.
- **States:** invalid key → inline error; daemon unreachable → "Can't reach Kiwi at <url>" with retry.

### 6.2 Jobs (table — primary list)
- **Elements:** sortable columns (status, goal, workflow, user, cost, duration, created); status chip using the 7 canonical states (Pending, Scheduling, Running, Paused, Succeeded, Failed, Canceled); row → Run Detail; per-row quick actions (cancel/retry). Toolbar: **filters** (status, user, workflow, date range), **free-text search**, **+ New Job**.
- **Scale:** server-side filter/sort + **cursor pagination** (horizon: 100k jobs).
- **States:** `empty` → onboarding panel (§5.1); `loading` → skeleton rows; `error` → retry banner; `live` → row status/cost update in place without full reload.

### 6.3 Submit Job (modal)
- **Elements:** workflow/template picker (browse built-in + org templates, read-only select), target inputs (repo/file, goal, test command), optional overrides shown from the selected template's manifest (models, limits) as read-only preview, **cost/limit preview** ("this counts against your monthly budget"). Sends an **idempotency key** so a double-click never double-submits.
- **States:** validation inline; over-quota → clear "concurrency/budget limit reached" with a link to Usage; success → navigate to Run Detail.

### 6.4 Run Detail (flagship)
**Header:** goal, status, elapsed, **live cost/token counter**, controls (Pause/Resume/Cancel/Retry/Re-run), **Share** (copyable deep link to job or a specific phase), failure cause on FAILED.

Tabs:
- **Timeline (default):** the live Actor→Critic event stream (§5.2). One stream renders both the semantic cards and (in Logs) the raw text — no duplicate feeds. Live indicator + auto-scroll with "jump to latest".
- **Agents:** master/worker topology (nodes + status + model), click a worker to filter the timeline to its events. Modest visualization, not a physics graph.
- **Logs:** raw streamed stdout/stderr; search within; download.
- **Changes:** workspace **diff viewer** (files added/modified), syntax-highlighted; the "did the agent change my code correctly?" trust surface; download patch.
- **Manifest:** read-only render of the immutable manifest (models, tools, egress allowlist, resource limits, secrets requested) — the audit/reproducibility artifact.
- **Cost:** per-job cost + input/output token rollup (per phase/agent breakdown where available).
- **Checkpoints:** list of checkpoints (step/time); **Resume from latest** action (arbitrary-checkpoint resume is roadmap).
- **States:** `loading` skeleton per tab; `live` streaming with reconnect; `error`/FAILED → failure panel with cause (budget/timeout/crash/DLQ) + Retry; PAUSED-for-secret → reconnect banner (§5.4).

### 6.5 Usage & Budget
- **Budget bar** (global, always truthful): month-to-date spend vs the org's real monthly limit, with warning thresholds.
- **Org usage (light):** spend trend, job volume, success/fail ratio — one screen, not a BI suite.
- **States:** empty (no spend yet) → friendly zero-state; error → retry.

### 6.6 Admin (admins only)
- **Users & Roles:** list, invite, remove, set role (member/admin).
- **API Keys:** create (token shown **once**), label, expiry, revoke.
- **Provider Config:** LLM provider + API key (write-only/masked) + per-role (actor/critic) model selection.
- **Limits:** editor for concurrency, per-job & monthly budget, timeout, sandbox disk.
- **Audit Log:** filter by actor/action/resource; the compliance trail.
- **States:** each list has empty/loading/error; destructive actions (revoke key, remove user) confirm; write success toasts.

### 6.7 Settings
- **Connection:** daemon URL + token (persisted locally).
- **Preferences:** theme (dark/light), density, timezone, live-vs-poll toggle.
- **My API Keys:** developer self-service keys for the CLI (distinct from org keys).

---

## 7. Cross-Cutting UX (the "product, not demo" layer)

- **Notifications.** Jobs run seconds→30 min; users leave the tab. Terminal-state toasts (succeeded/failed) + optional browser/email notifications; **budget-limit-hit** surfaces here too. A bell with an unread count and a recent-activity dropdown.
- **Onboarding & empty states.** No dead-end blank screens; every empty surface teaches the next action and links docs.
- **Error handling & toasts.** Auth failures, 429 budget rejections, tunnel drops, and stream disconnects render as clear human messages with a recovery action — never raw console errors.
- **Help & glossary.** Agent concepts (manifest, checkpoint, critic, worker, tunnel) get inline tooltips + a glossary; persistent link to docs.
- **Shareable deep links.** Every job and phase is URL-addressable ("look at this run") for team debugging.
- **Responsive / mobile-lite.** Monitoring and acting on a run works on a phone; tables collapse to cards.
- **Accessibility (WCAG 2.1 AA).** Full keyboard navigation, focus management, ARIA on live regions (the timeline announces new steps), sufficient contrast in both themes.
- **Theming.** Dark (default, matches the Kiwi brand) and light; respects OS preference.

---

## 8. Visual Design System

- **Brand:** Kiwi — dark-mode-first, premium/technical aesthetic (consistent with the existing marketing site and dashboard). Accent used for status/live indicators.
- **Status palette:** one color per canonical job state, reused everywhere (chips, timeline, board legends) for instant recognition.
- **Typography:** clear type scale; monospace for code, logs, tokens, and IDs.
- **Components (shared library):** status chip, cost/token badge, live-stream card, diff view, JSON/manifest viewer, data table (sortable/paginated), modal, toast/notification, empty-state, skeleton loader, confirm dialog, banner (paused/secret/error).
- **Motion:** subtle; a persistent "live" pulse on streaming surfaces; new timeline cards animate in.

---

## 9. Real-Time & Client Architecture

- **Transport:** consume the control-plane **event stream** (SSE/WebSocket) for the timeline, logs, topology, and live cost; fall back to polling if streaming is unavailable (surfaced via the live/poll toggle).
- **Resilience:** auto-reconnect with backoff; on reconnect, fetch missed events by sequence to avoid gaps (the stream carries per-job ordered `seq`); dedupe by event id.
- **Optimistic UI:** lifecycle actions (cancel/pause) reflect immediately with a pending state, reconciled by the next authoritative event.
- **Routing:** client-side routes are deep-linkable (`/jobs/:id`, `/jobs/:id/timeline#step-:seq`).
- **State:** per-run event log cached client-side for instant tab switches; large payloads (diffs, snapshots) fetched lazily.

---

## 10. Success Metrics
- Time-to-first-job for a new user (onboarding funnel).
- % of runs where the user opens the Timeline (flagship engagement).
- % of agent diffs reviewed before acceptance (trust).
- Notification opt-in / re-engagement rate on terminal states.
- Budget-surprise rate (jobs killed on budget with no prior warning view) → target ~0.
- Console task-completion without CLI (submit/cancel/retry done in-app).

---

## 11. Scope: v1 vs Roadmap vs Cut (decision record)

### v1 KEEP set (the launch-worthy experience)
Auth & shell: **API-key login**, **session badge/logout**.
Jobs: **table (primary) with filters, search, pagination**, **submit modal**, **template picker (browse)**.
Run detail (flagship): **job detail**, **Actor→Critic timeline (absorbs the separate "event feed")**, **master/worker topology**, **live logs**, **diff viewer**, **manifest viewer**, **per-job cost & tokens**, **failure panel**, **checkpoint list + resume-latest**.
Controls: **pause/resume**, **cancel**, **retry**, **re-run (replay manifest)**, **secret-tunnel reconnect UX**.
Cost/gov: **truthful budget bar**, **light org usage**, **users & roles**, **API key lifecycle**, **provider config**, **limits editor**, **audit viewer**.
Settings: **connection**, **preferences (incl. theme)**, **personal API keys**.
Table-stakes layer: **onboarding/empty states**, **notifications (incl. budget-hit)**, **help/tooltips/glossary**, **error/toast clarity**, **shareable deep links**, **responsive + accessibility baseline**.
External: **Grafana deep-links** (don't rebuild observability in-app).

### Roadmap (DEFER — real, but post-launch)
OIDC/SSO · org switcher (until multi-org membership is common) · saved filter views · trace/span waterfall · resume-from-arbitrary-checkpoint · cost-over-time & tokens-per-step charts · cost breakdown by user/workflow/model · in-app template authoring (CLI-first) · human-in-the-loop approval UX · self-serve org creation · operator fleet/health · global activity feed · status page · command palette · in-app feedback widget.

### Cut (won't build for the customer console)
- **Duplicate Kanban board** — the table is the better primary list; don't maintain two.
- **Separate live "event feed"** — it *is* the timeline rendered semantically; one stream, one view.
- **In-browser template/manifest editor** — authoring is CLI-scoped; a visual editor is a multi-sprint surface, not a launch bullet.
- **DLQ/poison-job inspector** — internal operator plumbing, not customer-facing.
- **Rebuilt in-app metrics dashboard** — delegate to Grafana/OTel; keep deep-links only.

### Rationale highlights (scope traps avoided)
1. Don't ship two job-list surfaces (kanban + table).
2. Don't smuggle operator/observability tooling (trace waterfall, DLQ, fleet health, metrics dashboard) into the *customer* console — deep-link to the standard stack instead.
3. Don't build in-browser authoring the platform intends to keep in the CLI.
4. Do add notifications — async jobs mean users won't watch a tab; missing this is the difference between a product and a demo.
5. Do add onboarding/empty-states/help/error/a11y — the layer that makes a new paying customer succeed on first login.

---

## 12. Appendix — Backend the v1 UX depends on (build, don't gate)

These are UX-driving dependencies; several map to endpoints whose *read* side exists today but whose *action* side (invite/role/revoke/limit-edit) is to build.

1. **Event stream API + SSE/WebSocket gateway** over the per-phase event log (phase/agent/seq) — powers the timeline, topology, live logs, live cost. *Highest leverage: the flagship depends on it.*
2. **Job detail read model** (job + agents + manifest + cost/tokens in one call).
3. **Jobs list query API** — server-side filter/sort, cursor pagination, free-text.
4. **Lifecycle actions** — `cancel | pause | resume | retry | replay`, plus checkpoint list + resume.
5. **Workspace diff API** from checkpoint snapshots (base vs latest).
6. **Manifest + workflow read APIs** — render config/audit and the submit template picker.
7. **Paused-for-secret signal** — job state carries a "waiting on <secret>" reason to drive the reconnect banner.
8. **Terminal-state + budget-hit notification hook** — webhook/poll → toast/email.
9. **Membership/roles read** — user→orgs and role, for the badge and admin.
10. **Deep-linkable job/phase IDs** in the read model.

Admin note: today's `/admin/*` routes are largely GET-only; the editing actions (invite user, set role, edit limits, revoke key) are to-build even where the read side exists.

---

## 13. Open Questions
- Notification channels for v1: in-app + browser only, or email from the start?
- Diff viewer source: live workspace vs. checkpoint snapshot diff — which is authoritative for an in-progress run?
- How much of the manifest is safe to render to non-admin members (secrets are names only, never values — confirm)?
- Mobile scope: full parity vs. monitor-and-act-only.
