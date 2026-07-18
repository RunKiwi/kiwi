# Kiwi — Dashboard UI Wiring Plan

> **Audience:** an implementation agent (the "worker"). Each task is self-contained.
> Do them **in order** within a phase; respect `Depends on`. Open **one PR per task**.
> A senior model does post-merge verification — leave clear PR descriptions and tests.
> This plan is about the **web UI**. The backend end-to-end plan is `TODO.md`.

---

## 0. Orientation (READ FIRST — cold-start brief)

**Goal.** Kiwi has a working BYOC backend (submit a task → daemon runs an Actor–Critic
loop in the customer's cloud → opens a GitHub PR) driven today only by the `kiwi` CLI. There
is a **Next.js dashboard shell in `frontend/` that is 100% mock** — no API calls, a
*simulated* login, and a stale data model. This plan **wires that shell to the real API** so
a user can log in, submit a task, and watch it reach a PR — in the browser.

**What exists (do not rebuild):**
- `frontend/` — a Next.js 15 App-Router app. Pages under `src/app/(dashboard)/`:
  `page.tsx` (home), `fleet/`, `topology/`, `models/`, `integrations/`, `settings/`,
  `onboarding/`; plus `src/app/login/page.tsx`. State in `src/store/useFleetStore.ts`
  (Zustand). Deps: `@xyflow/react` (node graphs), `recharts`, `lucide-react`, `zustand`.
  **It is all mock data.** `login/page.tsx` only *simulates* OAuth. The store models
  `master`/`worker` sub-agents (a design we RETIRED) and providers like `OpenAI`/`Cohere`.
- **Control Plane API** (Go, `pkg/orchestrator`), served by `kiwid`. The endpoints below
  already exist and are the UI's data source. Auth is a **Bearer org API key**.

**What is stale and must be corrected as you wire (do NOT preserve it):**
- The `master`/`worker` sub-agent model — gone. The real model is **`job → tasks[] →
  {status, result_url, result_detail}`**.
- Provider names — the supported providers are **Anthropic, Gemini, Codex**. Never write the
  literal three-letter "Open"+"AI" brand string anywhere (use `codex`). Remove `Cohere`/`Meta`.

**The auth reality.** The API authenticates with an **org API key** (`Authorization: Bearer
<key>`). There is **no** server-side session/OAuth. So the UI's v1 login is *"paste your API
key"*, validated against `GET /auth/validate`. Real GitHub OAuth is a much larger, separate
effort — **out of scope**; do not attempt it.

---

## 1. Conventions — apply to EVERY task

**Backend (Go) tasks — all must pass before commit:**
```bash
gofmt -l cmd/ pkg/                 # MUST print nothing (fix: gofmt -w cmd/ pkg/)
CGO_ENABLED=0 go vet ./...         # clean
CGO_ENABLED=0 go build ./...       # builds
CGO_ENABLED=0 go test ./pkg/...    # passes
```
- Every task-scoped query is **org-scoped** (`WHERE org_id = ?` via `claims.OrgID`).
- Ship tests with the feature (mirror `pkg/orchestrator/jobs_api_test.go` — `httptest` +
  the package's test store helper).

**Frontend tasks — all must pass before commit:**
```bash
cd frontend
npm ci
npm run lint       # eslint, MUST pass
npm run build      # next build, MUST pass (this is your main gate — no type errors)
```
- **Read `frontend/CLAUDE.md` and `frontend/AGENTS.md` first** (short, but they are the
  frontend's house rules).
- Never hardcode the API URL or a token in source. The base URL comes from
  `NEXT_PUBLIC_KIWI_API_URL` (default `http://localhost:8080`). The token comes from the
  logged-in user (localStorage), never committed.
- Keep TypeScript strict (no `any` where a real type exists). Reuse the existing component
  style (Tailwind classes already in the pages).

**Git/PR workflow (same as `TODO.md`):** branch `git checkout -b <type>/<slug>`; commit
messages end with `Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>`; open a PR to
`main`; end the PR body with `🤖 Generated with [Claude Code](https://claude.com/claude-code)`.
For frontend-only PRs that don't touch the root `README.md`, add the `skip-readme-check`
label. After opening the PR, STOP and report; wait before the next task.

**The exact API surface (verified — use these shapes literally):**

| Method & path | Auth | Request | Response |
| :--- | :--- | :--- | :--- |
| `POST /api/v1/planner/plan` | Bearer | `{task, repo_url, ref, file, test_cmd, model, max_workers}` | **202** `{manifest_id, job_id, task_ids[], summary}` |
| `GET /api/v1/jobs/{jobID}` | Bearer | — | `{job_id, tasks:[{id, status, result_url?, result_detail?}]}` |
| `GET /api/v1/jobs` | Bearer | — | **(build in UA1)** |
| `GET /api/v1/daemons` | Bearer | — | **(build in UA2)** |
| `POST /api/v1/credentials` | Bearer | `{name, kind, value}` | **204** No Content |
| `GET /auth/validate` | Bearer | — | `{user_id, org_id, org_name, ...}` (200 if key valid, 401 if not) |

`status` values on a task: `QUEUED | LEASED | SUCCEEDED | FAILED`. `result_url` is the PR
link (present on a delivered SUCCEEDED task). **`POST /api/v1/planner/plan` returns 202** —
treat any 2xx as success in the client.

---

## PHASE UA — Server endpoints the UI needs (Go)

### [DONE] UA1 — `GET /api/v1/jobs` : list the org's jobs

**Priority:** P0. **Depends on:** none. **Size:** S–M.

**Goal.** A dashboard needs a **list** of jobs; today only single-job read exists. Add a
list endpoint that aggregates each job's tasks into one row.

**Files.**
- `pkg/store/queue.go` — add `ListJobs(ctx, orgID string) ([]JobSummary, error)`.
- `pkg/store/store.go` — add `ListJobs` to the `Store` interface.
- `pkg/orchestrator/jobs_api.go` — add `handleJobsList`.
- `pkg/orchestrator/server.go` — register `GET /api/v1/jobs`. **Important:** the existing
  `mux.HandleFunc("/api/v1/jobs/", s.handleJobStatus)` matches the trailing-slash subtree;
  register the exact path `"/api/v1/jobs"` (no slash) for the list so both resolve.

**Implementation.**
1. `ListJobs`: query `queued_tasks WHERE org_id = ?`, group by `job_id`. For each job return
   a `JobSummary{ JobID string; CreatedAt time.Time; TaskCount int; Status string; PRURLs []string }`
   where `Status` is derived: `FAILED` if any task FAILED, else `SUCCEEDED` if all SUCCEEDED,
   else `RUNNING` if any LEASED, else `QUEUED`. `PRURLs` = the non-null `result_url`s. Order
   by newest job first (max `created_at`).
2. `handleJobsList`: resolve `claims`; 401 if nil; call `ListJobs(claims.OrgID)`; JSON-encode
   `{"jobs": [...]}`.

**Acceptance criteria.** `GET /api/v1/jobs` returns only the caller-org's jobs, newest first,
with a correct derived status and any PR URLs; another org's jobs never appear; 401 without a
valid key.

**Tests.** Store test: two orgs, mixed task statuses → correct per-org grouping + derived
status. Handler test: seeded jobs → correct JSON; missing claims → 401.

**Gotchas.** Do NOT return the raw `spec` (may hold internal detail) — only the summary
fields above. Org-scope strictly.

---

### [DONE] UA2 — `GET /api/v1/daemons` : list registered daemons (for the fleet page)

**Priority:** P1. **Depends on:** none. **Size:** S.

**Goal.** The fleet/topology page needs the org's daemons and their liveness.

**Files.** `pkg/store/` (add `ListDaemons(ctx, orgID) ([]Daemon, error)` + interface entry),
`pkg/orchestrator/daemon_api.go` or a new `daemons_list_api.go` (`handleDaemonsList`),
`server.go` (register `GET /api/v1/daemons`, behind `AuthMiddleware`).

**Implementation.** Return, per daemon: `{id, last_seen_at, created_at}` and a derived
`online` bool (`last_seen_at` within the last, say, 30s). **Do NOT return `sign_pub_key` /
`enc_pub_key`** — they are identity material, not needed by the UI.

**Acceptance criteria.** Returns only the caller-org's daemons with an `online` flag; 401
without a valid key; key material is never in the response.

**Tests.** Store + handler tests (org-scoped; online derivation).

---

## PHASE UB — Frontend foundation

### UB1 — API client + configuration

**Priority:** P0. **Depends on:** none. **Size:** S.

**Goal.** One typed place that talks to the Control Plane.

**Files.**
- `frontend/src/lib/api.ts` (new) — a `fetch` wrapper.
- `frontend/.env.local.example` (new) — document `NEXT_PUBLIC_KIWI_API_URL`.

**Implementation.** Export a `KiwiClient` (or plain functions) that:
- reads base URL from `process.env.NEXT_PUBLIC_KIWI_API_URL || "http://localhost:8080"`.
- reads the token from the auth store (UB2) and sets `Authorization: Bearer <token>`.
- has typed methods: `validate()`, `submitPlan(body)`, `getJob(jobID)`, `listJobs()`,
  `setCredential(name, kind, value)`, `listDaemons()`.
- throws a typed error on non-2xx (surfacing the server's message); **accepts 202** for
  `submitPlan`.

**Acceptance criteria.** `npm run build` passes; every method targets the exact path/shape in
§1; no URL or token is hardcoded.

**Tests.** N/A (thin wrapper) — but types must compile under `next build`.

---

### UB2 — API-key login (replace the simulated OAuth)

**Priority:** P0. **Depends on:** UB1. **Size:** S–M.

**Goal.** A real (if minimal) auth: paste an org API key, validate it, persist it, guard the
dashboard.

**Files.** `frontend/src/app/login/page.tsx` (rewrite), `frontend/src/lib/auth.ts` (new —
token get/set/clear in `localStorage` under key `kiwi_token`; a tiny `useAuth` hook),
`frontend/src/app/(dashboard)/layout.tsx` (redirect to `/login` when no token).

**Implementation.**
1. Login page: an input for the API key + "Continue". On submit, call `client.validate()`;
   on 200 store the token and the returned `{org_id, org_name}`, redirect to the dashboard;
   on 401 show "invalid key". Replace the `handleGithubLogin` simulation entirely. Keep a
   short note that GitHub OAuth is planned but not yet available.
2. Auth guard: the dashboard layout checks for a token client-side and redirects to `/login`
   if absent. Add a "Log out" action (clears the token) somewhere in the shell (e.g. settings
   or a header menu).

**Acceptance criteria.** With `kiwid` running and a valid key, login succeeds and lands on the
dashboard; an invalid key is rejected; visiting a dashboard route with no token redirects to
`/login`; logout clears state. `npm run build` passes.

**Gotchas.** localStorage is client-only — guard against SSR access (`typeof window`). Never
log the token.

---

### UB3 — Real data model (retire master/worker)

**Priority:** P0. **Depends on:** UB1. **Size:** S.

**Goal.** Replace the mock types in `useFleetStore.ts` with the real ones so pages bind to
truth.

**Files.** `frontend/src/store/useFleetStore.ts`, and any component importing the old types
(`TaskDrawer.tsx`, pages).

**Implementation.** New types:
```ts
type TaskStatus = "QUEUED" | "LEASED" | "SUCCEEDED" | "FAILED";
interface JobTask { id: string; status: TaskStatus; result_url?: string; result_detail?: string; }
interface Job { job_id: string; status: string; created_at: string; task_count: number; pr_urls: string[]; }
interface Daemon { id: string; online: boolean; last_seen_at?: string; created_at: string; }
interface ProviderConfig { name: "Anthropic" | "Gemini" | "Codex"; isConfigured: boolean; }
```
Remove `SubAgent`/`master`/`worker`, `PullRequest` mock, and `OpenAI`/`Cohere`/`Meta`. Add
store actions that call the UB1 client (`loadJobs`, `loadJob`, `loadDaemons`). Keep the store
holding server data only — no fabricated values.

**Acceptance criteria.** Project builds with the new types; no references to master/worker or
the removed provider names remain (`grep` clean). No literal "Open"+"AI" string anywhere.

---

## PHASE UC — Wire the pages

### [DONE] UC1 — Submit a task (the entry)

**Priority:** P0. **Depends on:** UB1–UB3. **Size:** M.

**Goal.** A form that submits a task to the real planner and shows the returned job id.

**Files.** A submit form — put it on the dashboard home `src/app/(dashboard)/page.tsx` or a
new `src/app/(dashboard)/submit/page.tsx`.

**Implementation.** Fields: `task` (textarea), `repo_url`, `ref` (default `main`), `file`,
`test_cmd`, `model` (a select — options: `claude-opus-4-8`, `claude-haiku-4-5-20251001`,
`gemini-2.0-flash`; the daemon routes `gemini-*` to Gemini, else Anthropic), `max_workers`
(default 1). On submit call `client.submitPlan(...)`; on success show the `job_id` and a link
to its job view (UC2); on error surface the server message.

**Acceptance criteria.** Submitting against a running `kiwid` enqueues a job and shows its id;
required-field validation matches the API (task/file/test_cmd required); errors are shown, not
swallowed. `npm run build` passes.

---

### [DONE] UC2 — Jobs list + detail (watch it reach a PR)

**Priority:** P0. **Depends on:** UA1, UC1. **Size:** M.

**Goal.** The core value view: a list of jobs and a detail that polls to completion and shows
the PR link.

**Files.** `src/app/(dashboard)/page.tsx` or `fleet/page.tsx` for the list; a job detail at
`src/app/(dashboard)/jobs/[jobId]/page.tsx` (new). May reuse `TaskDrawer.tsx`.

**Implementation.**
- List: `client.listJobs()` → table of `{job_id, status, created_at, pr_urls}`; row links to
  detail; refresh every few seconds.
- Detail: `client.getJob(jobId)`, **poll every 2–3s** until all tasks terminal; render each
  task's `status`, and when `result_url` is present show a prominent **"View PR"** link; when
  `FAILED`, show `result_detail` (the reason). Stop polling on terminal state.

**Acceptance criteria.** After UC1 submit + a running daemon, the detail view transitions
`QUEUED → … → SUCCEEDED` and shows the PR link (or `FAILED` + reason). No other org's jobs are
visible. `npm run build` passes.

**Gotchas.** Clear the poll interval on unmount and on terminal state (no leaks).

---

### [DONE] UC3 — Credentials (settings)

**Priority:** P1. **Depends on:** UB1–UB3. **Size:** S.

**Goal.** Let the user store `ANTHROPIC_API_KEY` / `GEMINI_API_KEY` / `GIT_TOKEN` from the UI.

**Files.** `src/app/(dashboard)/settings/page.tsx`.

**Implementation.** Three inputs (Anthropic key, Gemini key, Git token) → `client.setCredential`
with names `ANTHROPIC_API_KEY` (kind `llm`), `GEMINI_API_KEY` (kind `llm`), `GIT_TOKEN`
(kind `git`). On save show success; never render a stored value back (the API never returns
it). Mask inputs (`type="password"`).

**Acceptance criteria.** Saving a credential returns 204 and shows success; values are never
echoed; `npm run build` passes.

---

### UC4 — Fleet page (daemons) — optional

**Priority:** P2. **Depends on:** UA2, UB3. **Size:** S.

**Goal.** Show registered daemons and liveness on `fleet/` or `topology/`.

**Implementation.** `client.listDaemons()` → cards/nodes with `id`, `online` badge,
`last_seen_at`. If using `@xyflow/react` topology, bind nodes to real daemons (drop the mock
AWS/GCP/cpu/ram fields unless the API provides them — it does not, so omit them).

**Acceptance criteria.** Real daemons render with correct online state; no fabricated metrics.

---

## PHASE UD — CORS & running locally

### UD1 — Dev run + CORS docs

**Priority:** P0 (needed to test anything). **Depends on:** none. **Size:** S.

**Goal.** Make the browser→API path work locally and document it.

**Implementation.** The Go server already honors `KIWI_CORS_ALLOWED_ORIGINS`. Document in
`frontend/README.md` (and the root README "Dashboard" section) the dev recipe:
```bash
# Control Plane with CORS for the dev UI origin
KIWI_CORS_ALLOWED_ORIGINS=http://localhost:3000 ./kiwid -addr :8080 -dsn "..."
# Frontend
cd frontend && cp .env.local.example .env.local   # set NEXT_PUBLIC_KIWI_API_URL=http://localhost:8080
npm ci && npm run dev                               # http://localhost:3000
```

**Acceptance criteria.** Following the doc, the dashboard at `:3000` logs in and loads jobs
from `:8080` with no CORS error.

---

## First slice (the demoable UI) & order

**Minimum to see a task go from submitted → PR link in the browser:**
`UD1 (CORS/run)` → `UB1 (client)` → `UB2 (login)` → `UB3 (model)` → `UA1 (list jobs)` →
`UC1 (submit)` → `UC2 (jobs view)`. Then `UC3 (creds)`, `UA2`+`UC4` (fleet), polish.

Verify the slice against a **running Control Plane + daemon** (see `TODO.md` / `deploy/` and
the CLI e2e), not just `npm run build`: log in with an org API key, submit a task, watch the
job view reach a PR link.
