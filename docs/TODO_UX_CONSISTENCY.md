# UX Consistency — Audit & Remediation TODO

**Context.** Kiwi is a **managed-first, BYOC-as-graduation** platform. Every signup is
a Free org that runs on a Kiwi-operated **shared fleet** (`shared-free`), graduating to
Pro/BYOC for a dedicated fleet. The dashboard, however, was built for the **BYOC
self-service** model ("create fleets, mint join tokens, register your own daemons") and
still shows that surface to *everyone, ungated by plan*. The two mental models are stacked
on top of each other, so Free users see controls that do nothing and can't see where their
work actually runs.

**The single organizing principle for every fix below:** **the plan drives the surface —
never render a control that does nothing for this plan.** Free = managed = a read-only
"you run here, on Kiwi" surface + usage + upgrade path. Pro/BYOC = the fleet/daemon/token
CRUD, tightened. `frontend/src/components/PlanUsage.tsx` is the *reference* for the right
pattern (it already special-cases `plan === "free"` cleanly) — mirror it.

**Mandatory checks (CLAUDE.md §2) — run before every commit; any failure is a blocker:**
```bash
gofmt -l cmd/ pkg/                 # MUST print nothing (fix: gofmt -w cmd/ pkg/)
CGO_ENABLED=0 go vet ./...         # MUST be clean
CGO_ENABLED=0 go test ./pkg/...    # MUST pass
CGO_ENABLED=0 go build ./...       # MUST build
cd frontend && npx tsc --noEmit    # MUST be clean (frontend changes)
```
Tests-first. Never write the literal Open+AI brand string (use `codex`). Keep
zero-knowledge a **BYOC-only** claim. Update `README.md` or label the PR `skip-readme-check`.

---

## The findings (severity-ranked; work them major → minor)

### MAJOR — the model contradicts itself

**M1. Free orgs can create fleets that never receive work.**
The Fleets page (`frontend/src/app/(dashboard)/fleet/page.tsx`) and the Command Center
"Fleet" selector (`app/(dashboard)/page.tsx`, Advanced) expose fleet create/select to Free
orgs. But the planner force-routes all Free work to `shared-free`
(`pkg/planner/handler.go:56-57`), overriding `req.FleetID`. The backend
`handleFleets` POST (`pkg/orchestrator/dashboard_api.go:26`) has **no plan gate**. Result:
a Free user builds inert fleets and picks fleets that are ignored.
**Fix:** hide fleet CRUD + the fleet selector for Free plan; backend rejects Free
`POST /api/v1/fleets` with a clear 403.

**M2. The fleet where Free work actually runs is invisible.**
`shared-free` is a routing constant (`pkg/auth/models.go:14`), not a `fleets` row, so it
never appears in `ListFleets`. A Free user has no way to see "my work runs on Kiwi's shared
fleet."
**Fix:** for Free plan, replace the fleet list with a read-only **Runtime** card:
"Running on Kiwi Managed (shared) · ● daemon online · N/500 agent-min · Upgrade for a
dedicated fleet." Source health from the org's daemon(s); usage from `GET /api/v1/usage`.

**M3. Onboarding is orphaned — new users never see it.**
After OAuth callback (`app/auth/callback/page.tsx:34`) and login
(`app/login/page.tsx:48`) users land on `/`. Nothing routes to `/onboarding`, the *only*
page that explains Free plan / shared fleet / BYO-key.
**Fix:** route first-time users to `/onboarding` (e.g. gate on "no repo connected / no key
stored / never submitted"), or fold its content into the empty-state of `/`.

**M4. `/fleet/deploy` is an orphan page that mints a FAKE API key.**
`app/(dashboard)/fleet/deploy/page.tsx` — component misnamed `OnboardingPage`; generates a
client-side `kw_live_…` string that is **never registered with the backend**; and prints
Terraform referencing non-existent modules (`runkiwi/swarm/aws` v1.0.0). Nothing links to
it. It looks real and does nothing — actively misleading.
**Fix:** delete the page, or rebuild it as a real BYOC-deploy flow (real minted token via
the join-token endpoint, real module source) reachable only from a BYOC fleet card.

**M5. "Managed" ⇔ `self-managed` naming is backwards.**
The UI shows **"Managed"** (Kiwi-operated) but writes DB type `self-managed`
(`FleetSelfManaged = "self-managed"`, `pkg/store/dashboard_models.go:13`). "self-managed"
reads as *"I run it myself"* = BYOC — the exact opposite. This footgun spans store,
provisioner, topology, and fleet UI.
**Fix (separate, more invasive):** rename the constant/value to something honest
(`FleetManaged = "managed"`), migrate existing rows, and update every `=== "byoc"` / label
site. Do this as its own PR *after* the plan-gating lands, with a data migration.

### MODERATE — right model, wrong details

**M6. "Generate join token" appears on Managed fleet cards** (`fleet/page.tsx:97`). Join
tokens enroll a *BYOC* daemon; Managed daemons are launched by the provisioner. Show the
button only on `type === "byoc"` cards; show provisioning status on Managed cards.

**M7. Empty-daemons state instructs the wrong audience** (`fleet/page.tsx:117`): "Create a
BYOC fleet and register a daemon… or run the managed daemon." A Free/managed user never
runs a daemon. Make the copy plan-aware.

**M8. Onboarding "Connect GitHub" is a fake stub** (`onboarding/page.tsx:12`, setTimeout →
next step), while the *real* GitHub connect is a PAT on Integrations
(`integrations/page.tsx`). Two inconsistent GitHub-connect UXs; onboarding implies a GitHub
App install that doesn't exist. Point onboarding at the real Integrations flow.

**M9. "Upgrade to Pro" is a dead-end stub** (`onboarding/page.tsx:18` → just redirects to
`/settings`; no checkout). Either wire a real billing flow or make the button honestly say
"contact us / coming soon" until it exists. (Tracks the deferred billing work.)

**M10. "Activate to run" error copy is paid-tier logic leaking to Free**
(`app/(dashboard)/page.tsx:242-246`). It fires on a 402/"activate" submit error a Free org
never hits, and tells the user to "activate to run" — but Free runs *without* activation.
Scope this message to paid plans; for Free show the real gate (suspended / over-limit).

### MINOR — polish & coherence

**M11. Nav label mismatches:** sidebar "Fleet" vs page "Fleets"; "Command Center" vs route
`/` (`app/(dashboard)/layout.tsx:36-43`). Pick consistent names.

**M12. Topology leaks the raw `shared-free` id** and has no node for the shared fleet
(`app/(dashboard)/topology/page.tsx`): with no `fleets` row, Free jobs/daemons hang off
"Control Plane" and the detail panel prints the literal string `shared-free`. Render a
synthetic "Kiwi Shared Fleet" node for Free, or a friendly label.

**M13. Plan badge duplicated & styled differently** — Settings "Status" block and
`PlanUsage` both render a plan chip. Consolidate to one source.

**M14. Default task-form models are Anthropic** (`DEFAULT_PLANNER_MODEL=claude-opus-4-8`,
`DEFAULT_WORKER_MODEL=claude-haiku-…`, `frontend/src/lib/api.ts:267`), but BYOK Free users
typically have Gemini (and the Anthropic key is out of credits per project memory). Consider
defaulting to a model that matches the key the org has actually stored, or nudging via the
Models page.

**M15. Integrations "sealed to your daemons" copy is BYOC-framed**
(`integrations/page.tsx:51`). For managed Free the daemon is Kiwi-operated; keep the
encryption claim but align the vocabulary with the managed mental model (and never imply
zero-knowledge for managed — CLAUDE.md §1).

> **Already addressed:** the Settings "INACTIVE" badge for Free orgs (a symptom of the same
> paid-vocabulary leak) is fixed in PR #224.

---

## Suggested phasing (one PR per phase, tests first)

- **Phase 1 — Plan-gate the fleet surface (M1, M2, M6, M7).** Frontend: Free plan sees the
  read-only Runtime card; Pro sees the current CRUD with join-token-on-BYOC-only and
  plan-aware empty state. Backend: reject Free `POST /api/v1/fleets` (403) — add a handler
  test. This one change kills the dead-fleet, invisible-runtime, and wrong-empty-state
  problems together.
- **Phase 2 — Fix the onboarding path (M3, M8, M9).** Route first-time users to onboarding;
  make "Connect GitHub" use the real Integrations flow; make "Upgrade to Pro" honest.
- **Phase 3 — Kill or rebuild `/fleet/deploy` (M4).** Delete the fake-key page, or rebuild
  as a real BYOC-only deploy flow.
- **Phase 4 — Copy & label coherence (M10, M11, M13, M14, M15).** Plan-aware error copy,
  consistent nav names, de-duplicated plan badge, sensible model defaults, aligned
  Integrations vocabulary.
- **Phase 5 — The `self-managed` rename (M5).** Its own PR with a data migration; touch
  store constant + every consumer (provisioner, topology, fleet UI). Do last — it's the most
  invasive and the least user-visible.
- **Phase 6 — Topology shared-fleet node (M12).** Synthetic node/label for `shared-free`.

## Definition of done

A Free user, on every page, sees a coherent managed story: they know their work runs on
Kiwi's shared fleet, they see their usage and limits, they're never shown a control that
does nothing, and the only "advanced" surface (BYOC fleets, join tokens, daemon deploy)
appears when — and only when — their plan actually uses it. A Pro/BYOC user sees the full
fleet CRUD, correctly labelled.

---

## One-shot prompt (paste to a fresh implementation agent)

> You are implementing **Phase 1** of `docs/TODO_UX_CONSISTENCY.md` in the Kiwi repo. Read
> that file's Context and Principle sections first, then implement **Phase 1 only**
> (plan-gate the fleet surface: findings M1, M2, M6, M7).
>
> **The principle:** the plan drives the surface — never render a control that does nothing
> for this plan. `frontend/src/components/PlanUsage.tsx` is the reference for reading the
> plan (`GET /api/v1/usage` → `plan`) and special-casing Free.
>
> **Frontend (`frontend/src/app/(dashboard)/fleet/page.tsx`):**
> - When `plan === "free"`: hide the create-fleet form and the org-fleet CRUD. Instead show
>   a read-only **Runtime** card — "Running on **Kiwi Managed** (shared) · ● daemon
>   online/offline · N / limit agent-min this month · *Upgrade for a dedicated fleet*" —
>   sourcing health from `client.listDaemons()` and usage from `client.getUsage()`.
> - When `plan !== "free"`: keep the current CRUD, but render "Generate join token" **only**
>   on `type === "byoc"` cards, and make the empty-daemons copy plan-aware (managed daemons
>   are launched by Kiwi; only BYOC needs a join token).
> - Also gate the Command Center "Fleet" selector (`app/(dashboard)/page.tsx`, Advanced) —
>   hide it for Free (their work always routes to `shared-free`).
>
> **Backend (`pkg/orchestrator/dashboard_api.go`, `handleFleets`):** reject
> `POST /api/v1/fleets` for a Free-plan org with `403` and a clear message (look up
> `Organization.Plan` for `claims.OrgID`). Add a handler test asserting Free → 403 and
> Pro → 201 (mirror `pkg/orchestrator/dashboard_api_test.go:TestHandleFleets`).
>
> Run all CLAUDE.md §2 checks plus `cd frontend && npx tsc --noEmit`; all must be clean.
> Ship the backend test. Open one PR titled `feat(ux): plan-gate the fleet surface` with a
> description mapping the change back to findings M1/M2/M6/M7. Do **not** start other phases.
