# Kiwi — Self-Serve Signup & Tenancy Work Plan

> **Audience:** an implementation agent (the "worker"). Tasks are self-contained.
> Do them **in order**; respect `Depends on`. Open **one PR per phase**. A senior
> model verifies post-merge — ship tests first, write clear PR descriptions.
> Obey `CLAUDE.md`. Read the RFC first:
> **[docs/rfcs/2026-07-19-self-serve-signup-and-tenancy.md](docs/rfcs/2026-07-19-self-serve-signup-and-tenancy.md)**.

---

## 0. Orientation (READ FIRST — cold-start brief)

**What this plan is.** Add **self-serve signup** to Kiwi: people sign in with
OAuth, get an org, and can plan tasks — but **running** a task is gated on
**activation** (payment or admin approve), which is what triggers the per-org
daemon VM (Managed GCP RFC). This decouples *org creation* (free, instant) from
*capacity provisioning* (expensive), so signups don't each spin a VM.

**The load-bearing rule:** creating an org ≠ provisioning a VM. Signup writes rows;
activation provisions capacity.

**What exists today (read it):**
- `pkg/auth/` — `Organization{id, name}`, `User{email, org_id, role}`,
  `APIKey{key_hash, user_id}` (`models.go`); admin CRUD in `admin.go` gated by
  `KIWI_SERVER_TOKEN`; `AuthMiddleware` resolves a bearer token → `UserClaims`.
- `pkg/store/models.go` also declares `Organization`/`User` (**two structs, same
  `organizations`/`users` tables** — reconcile carefully; add columns once, keep
  both struct definitions consistent, or consolidate).
- `OrgLimits` (per-job budget, concurrency, timeout) in `pkg/store`.
- The **managed provisioner** `cmd/opsctl` (mints an internal join token →
  per-org GCE VM) from the Managed GCP work — call it from the activation hook.
- Frontend login only **simulates** OAuth (`frontend/src/app/login/`).

**Non-negotiable (from the RFC):**
- **v1 is pay-to-run.** No free execution. Free accounts can sign up, connect
  repos, and plan/preview; running requires `activation_state = active`.
- **API keys stay** for CLI/CI; OAuth is for humans. OAuth mints an initial key.
- **Backward compatible.** The `/admin/*` path, existing API keys, and
  `make local` must keep working. New env (OAuth client IDs/secrets) unset ⇒
  OAuth routes are simply disabled, not a crash.
- Never trust an **unverified** email domain for routing; use the IdP-verified
  address. Default **domain-join OFF** (request+approve) until an org opts in.
- OAuth secrets come from env/Secret Manager — never committed.

**Pre-commit checks (all must pass before every commit):**
```bash
gofmt -l cmd/ pkg/           # prints nothing
CGO_ENABLED=0 go vet ./...
CGO_ENABLED=0 go test ./pkg/...
CGO_ENABLED=0 go build ./...
# Frontend phases: cd frontend && npm run lint && npm run build
```
Label each PR `skip-readme-check` unless it edits the root `README.md`.

---

## Phase S0 — Org/User model extensions + migration (code)

**Depends on:** nothing. **PR:** `feat(auth): tenancy fields on org and user`.

**Do:**
1. Add columns to the `organizations` table (via a new `migrations/00NN_*.up.sql`
   + `.down.sql`, and keep the GORM struct(s) in sync):
   - `type TEXT NOT NULL DEFAULT 'personal'` (`personal|team`)
   - `primary_domain TEXT NOT NULL DEFAULT ''`
   - `domain_join BOOLEAN NOT NULL DEFAULT false`
   - `plan TEXT NOT NULL DEFAULT 'free'`
   - `activation_state TEXT NOT NULL DEFAULT 'inactive'` (`inactive|active|suspended`)
2. Add to `users`: `oauth_provider TEXT`, `oauth_subject TEXT` (nullable), with a
   unique index on `(oauth_provider, oauth_subject)`.
3. Reconcile the duplicate `Organization`/`User` structs (`pkg/auth` and
   `pkg/store`) so both reflect the new columns — do **not** let them drift.
4. Constants/enums for the string fields; a helper `Org.CanRun() bool`
   (`activation_state == active`).

**Acceptance:** migration applies up and down; existing rows default to
`personal`/`free`/`inactive`; both struct sets read/write the new columns.

**Tests:** store tests for the new fields + defaults; `CanRun()` unit test.

---

## Phase S1 — GitHub + Google OAuth sign-in (code)

**Depends on:** S0. **PR:** `feat(auth): oauth sign-in + sessions`.

**Do:**
1. `pkg/auth/oauth.go`: OAuth2 flows for **GitHub** (primary; request `repo`
   scope) and **Google** (secondary). Config from env
   (`KIWI_GITHUB_OAUTH_CLIENT_ID/SECRET`, `KIWI_GOOGLE_OAUTH_*`,
   `KIWI_OAUTH_REDIRECT_BASE`). If a provider's env is unset, its routes 404 /
   are hidden — never crash.
2. Handlers: `GET /auth/{provider}/start` (state cookie, redirect) and
   `GET /auth/{provider}/callback` (verify state, exchange code, fetch verified
   email + name). Reject unverified emails.
3. On callback: resolve/create the `User` (by `(provider, subject)` then email),
   route to an `Org` (stub for S2 — for now create a personal org), create a
   **browser session** (signed, HTTP-only, Secure, SameSite cookie), and **mint
   an initial API key** returned once to the user.
4. `AuthMiddleware` also accepts a valid session cookie (in addition to bearer
   API keys) and resolves the same `UserClaims`.

**Acceptance:** a stubbed OAuth provider drives start→callback→session→dashboard;
API-key auth is unchanged; unset provider env disables that provider cleanly.

**Tests:** the callback flow with the provider's token/userinfo endpoints stubbed
via `httptest` (no real GitHub/Google); state-mismatch and unverified-email
rejections; session cookie issuance + middleware acceptance.

**Security:** CSRF via state param; `Secure`+`HttpOnly`+`SameSite=Lax` cookies;
no tokens in logs.

---

## Phase S2 — Domain routing (personal vs company) (code)

**Depends on:** S1. **PR:** `feat(auth): route signups to personal vs company orgs`.

**Do:**
1. Embed a **maintained public-email-provider list** (a data file) and
   `isPersonalDomain(domain) bool`.
2. `resolveOrgForUser(ctx, email) (*Org, isNew bool, needsApproval bool)`:
   - **personal domain** → create/return the user's **own personal org**;
   - **company domain** → find the org with `primary_domain == domain`; if none,
     **create the team org** (this user becomes admin); if one exists, route per
     S3 (auto-join vs request).
3. Wire it into the S1 callback (replace the personal-org stub).

**Acceptance:** a `@gmail.com` signup gets a personal org; the first `@acme.com`
signup creates the acme team org; a second `@acme.com` routes to it (join handled
in S3).

**Tests:** table tests over personal/company domains and first/second-user cases,
with the org store faked or in-memory.

---

## Phase S3 — Company-domain join (code)

**Depends on:** S2. **PR:** `feat(auth): company domain join (auto / request-approve)`.

**Do:**
1. If the target company org has `domain_join = true` → **auto-join** as `member`.
2. Else create a **join request** (new `org_join_requests` table:
   `{org_id, user_email, status, created_at}`) and an admin endpoint to
   **list/approve/deny**; on approve, attach the user as `member`.
3. Admin setting to toggle `domain_join`.

**Acceptance:** with domain-join on, second `@acme.com` user is a member
immediately; with it off, they're pending until an admin approves.

**Tests:** both paths; approving a request attaches the user; deny leaves them
without org access.

---

## Phase S4 — Activation gate on execution (code)

**Depends on:** S0. **PR:** `feat(exec): gate task execution on activation`.

**Do:**
1. In the **submit/run path** (`planner` HTTP handler and/or the task-create
   handler in `pkg/orchestrator`), check `Org.CanRun()`. If the org is
   **inactive/suspended**, return **`402 Payment Required`** with a clear message
   ("activate to run"). **Planning/preview must still work** while inactive — only
   the step that actually enqueues execution is gated.
2. Add an **activation transition** function `ActivateOrg(orgID)` /
   `SuspendOrg(orgID)` that sets `activation_state` and enqueues a
   **provisioning request** (a row an operator/worker fulfils by running
   `cmd/opsctl`, per Managed RFC §3.2.1 — **no autoscaler**). Deactivation enqueues
   a reclaim request.

**Acceptance:** an inactive org can plan but gets `402` on run; activating it
flips state and records a provisioning request; suspending records a reclaim.

**Tests:** handler tests for the `402` gate (inactive) vs allowed (active);
`ActivateOrg`/`SuspendOrg` state transitions + request enqueue.

---

## Phase S5 — Billing → activation (code)

**Depends on:** S4. **PR:** `feat(billing): checkout webhook flips activation`.

**Do:**
1. A billing-provider abstraction (Stripe-shaped) behind an interface; a
   **webhook handler** that, on a successful checkout/subscription event, calls
   `ActivateOrg`. Verify webhook signatures; make it **idempotent** (replays must
   not double-activate).
2. A **manual-approve** admin action that calls the same `ActivateOrg` (for
   design partners / sales-led deals) — the identical state transition without a
   payment.
3. Map `plan` → `OrgLimits` (free/individual/team caps).

**Acceptance:** a stubbed webhook activates the org and sets plan/limits; replayed
events are no-ops; manual approve reaches the same state.

**Tests:** webhook signature verification, idempotency, and the plan→limits map,
with the billing provider stubbed.

---

## Phase S6 — Frontend: real login & onboarding (frontend)

**Depends on:** S1–S5. **PR:** `feat(frontend): real oauth login, onboarding, org management`.

**Do:**
1. Replace the **simulated** login with real "Continue with GitHub/Google"
   buttons hitting `/auth/{provider}/start`.
2. Onboarding for a new org: connect a repo, run a **preview**, then an
   **"Activate to run"** call-to-action (checkout or "contact us" for manual).
3. Org/team management: show plan + `activation_state`, seat list, pending join
   requests (approve/deny), the `domain_join` toggle.
4. Surface the `402` "activate to run" clearly on the submit path.

**Acceptance:** a user can sign in with GitHub, land in onboarding, preview a task,
and see the activation gate; an admin can manage seats/join requests.

**Tests:** `npm run lint` + `npm run build` green; component-level checks where
practical.

---

## Sequencing & launch gate

- **Data + auth:** S0 → S1 → S2 → S3.
- **Money path:** S4 → S5.
- **Surface:** S6.

**Self-serve v1 launch gate** = S0–S2 + S4 (pay-to-run gate) + S6, with activation
via **manual approve** (S5's billing webhook can follow). Company-domain join (S3)
and automated billing (S5) can land shortly after.

Broader sequencing (RFC §8): hand-provisioned design partners (no signup) →
**self-serve pay-to-run (this plan)** → free shared-pool tier (only after the
Managed RFC's M2 isolation).

## Guardrails

- No real GitHub/Google/Stripe calls in tests — interfaces + `httptest` stubs.
- OAuth/billing secrets from env; never committed; unset provider env disables it.
- Sessions are security-sensitive: CSRF state, `Secure`/`HttpOnly`/`SameSite`, no
  tokens in logs.
- Keep it backward compatible: `/admin/*`, API-key auth, and `make local` unchanged.
- One PR per phase, tests first, `skip-readme-check` unless touching root README.
