# Kiwi — End-to-End + SaaS Deployment Work Plan

> **Audience:** an implementation agent (the "worker"). Each task is self-contained.
> Do them **in order** within a phase; respect `Depends on`. Open **one PR per task**.
> A senior model does post-merge verification — so leave clear PR descriptions and tests.

---

## 0. Orientation (READ FIRST — this is a cold-start brief)

**What Kiwi is.** A BYOC (Bring-Your-Own-Cloud) agentic execution platform. A SaaS
**Control Plane** (`cmd/kiwid`, `pkg/orchestrator`) plans work and hands it out via a
Postgres **lease queue** (`pkg/store/queue.go`). A **Data Plane daemon** (`cmd/kiwidaemon`,
`pkg/daemon`) runs in the customer's cloud, leases tasks over HTTPS, clones the repo, runs
an **Actor–Critic loop** (`pkg/loop`) in a sandbox until a test command passes, and reports
back. Credentials are sealed to the daemon's X25519 key (`pkg/crypto`); the Control Plane
never needs plaintext for BYOC.

**The end-to-end goal of this plan.** Make ONE task flow all the way through and produce a
visible result, then make it deployable as SaaS:

```
kiwi submit -repo <url> -file <f> -test-cmd <t> -task "<goal>"
  → Control Plane: 1-worker plan → lease queue
  → daemon: lease → clone worktree → Actor–Critic loop → test green
  → daemon: commit → push branch kiwi/<jobID> → open GitHub PR
  → CLI: prints the PR URL
```

**What already works (do not rebuild).**
- The seam is connected: daemon registers (single-use join token), heartbeat-polls
  `/api/v1/daemon/heartbeat`, leases one task, receives sealed creds, opens them via
  `crypto.OpenSealed`, runs the loop, reports to `/api/v1/daemon/result`.
- DAG dependencies are enforced in `LeaseNextTask` (a task leases only when its
  `depends_on` all SUCCEEDED).
- `kiwi submit -repo` posts to `/api/v1/planner/plan`; the heuristic planner emits a
  **single executable worker** (MVP) carrying `repo_url`, `ref`, `file`, `test_cmd`.
- The daemon honors `USE_DOCKER` (isolation on by default; `USE_DOCKER=false` runs the
  test command locally).

**In-flight PRs this plan assumes are merged to `main` first (baseline):**
`#136` (daemon USE_DOCKER fix), `#135` (DAG enforcement), `#137` (CLI `-repo` BYOC entry).
If they are not yet merged, branch your work off a local branch that includes them, and say
so in your PR.

**What is still missing (this plan fills it):** a way to store customer credentials
(Anthropic key + git token), the daemon actually delivering a PR, a way for the CLI to see
the result, and everything needed to run the Control Plane as a hosted SaaS.

---

## 1. Conventions — apply to EVERY task

**Mandatory pre-commit checks (all must pass; treat any failure as a hard blocker):**
```bash
gofmt -l cmd/ pkg/                 # MUST print nothing (fix: gofmt -w cmd/ pkg/)
CGO_ENABLED=0 go vet ./...         # MUST be clean
CGO_ENABLED=0 go build ./...       # MUST build
CGO_ENABLED=0 go test ./pkg/...    # MUST pass
```

**Hard rules (from `CLAUDE.md` — violating these fails review):**
- Language Go 1.25, persistence PostgreSQL via GORM, queue = Postgres lease queue.
- Every task-scoped DB row carries `org_id`; every query is org-scoped.
- Secrets are never persisted in plaintext; encryption-at-rest uses `KIWI_ENCRYPTION_KEY`
  (32-byte hex) via `crypto.EncryptAtRest`/`DecryptAtRest`.
- **Never** write the literal three-letter "Open"+"AI" brand string in code/config/docs.
  Use `codex` for that provider. (`OpenAIAPIKey` already exists in a struct — do not add
  new occurrences of the brand string.)
- **Do not** claim "zero-knowledge" for the managed tier (only BYOC is zero-knowledge).
- Wire types shared between daemon and Control Plane live in `pkg/daemon/types.go` and are
  the **single source of truth** — the orchestrator imports them. Change them there only.
- Tests ship **with** the feature (write them in the same PR). Use `httptest`/fakes for
  network and a local bare git repo for git operations — **no real network in tests**.
- Keep `README.md` updated in the same PR, or add the `skip-readme-check` label to the PR.

**Git/PR workflow per task:**
1. Branch off `main` (or the baseline branch): `git checkout -b <type>/<slug>`.
2. Implement + tests. Run all four checks above.
3. Commit. End commit messages with:
   `Co-Authored-By: Claude Opus 4.8 <noreply@anthropic.com>`
4. `git push -u origin <branch>` and open a PR to `main` with a description covering:
   what/why, the acceptance criteria you met, and how you tested. End PR body with:
   `🤖 Generated with [Claude Code](https://claude.com/claude-code)`
5. If the change is docs-only or needs no README update, add label `skip-readme-check`
   via: `gh api -X POST repos/RunKiwi/kiwi/issues/<PR#>/labels -f "labels[]=skip-readme-check"`

**Credential name conventions (used across tasks):**
- Anthropic API key → credential name `ANTHROPIC_API_KEY`, kind `llm`. Constant already
  exists in `pkg/daemon` as `anthropicKeyName`. The daemon withholds it from the sandbox.
- Git push token → credential name `GIT_TOKEN`, kind `git`. Used **daemon-side** for push
  and PR creation, never injected into the sandbox.

---

## PHASE A — Make it work end-to-end (functional)

### A1 — Credential set-surface (`kiwi creds set` → HTTP → store) [DONE]

**Priority:** P0. **Depends on:** baseline. **Size:** S.

**Goal.** Let a customer store their org's credentials (Anthropic key, git token) so the
Control Plane can seal them to the daemon. The storage layer already exists
(`store.SaveCredential(ctx, orgID, name, kind, plaintext)` in `pkg/store/credentials.go`,
encrypt-at-rest); only the HTTP endpoint and CLI are missing.

**Why.** Without this the daemon's loop has no Anthropic key (falls back to smoke) and no
git token (cannot push). This unblocks A3.

**Files.**
- `pkg/orchestrator/credentials_api.go` (new) — HTTP handler.
- `pkg/orchestrator/server.go` — register the route on `mux` (behind `AuthMiddleware`).
- `pkg/client/client.go` — new `SetCredential` client method.
- `cmd/kiwi/creds.go` (new) — `kiwi creds set` subcommand.
- `cmd/kiwi/main.go` — add `case "creds"` to dispatch.
- `README.md` — document `kiwi creds set`.

**Implementation.**
1. Endpoint `POST /api/v1/credentials`, JSON body `{"name":string,"kind":string,"value":string}`.
   - Resolve org via `auth.ClaimsFromContext(r.Context())`; 401 if nil. Use `claims.OrgID`.
   - Validate `name` and `value` non-empty; reject `name` not matching `^[A-Z0-9_]+$`.
   - Call `s.storage.SaveCredential(r.Context(), claims.OrgID, name, kind, value)`.
   - Respond `204 No Content` (never echo the value back).
2. Register in `server.go` next to the planner route:
   `mux.HandleFunc("/api/v1/credentials", s.handleSetCredential)`.
3. Client `func (c *Client) SetCredential(ctx, name, kind, value string) error` — POST JSON,
   Bearer token, expect 204.
4. CLI: `kiwi creds set <name> <value> [-kind <kind>]`. Add friendly aliases so
   `kiwi creds set anthropic <key>` maps to name `ANTHROPIC_API_KEY` kind `llm`, and
   `kiwi creds set git <token>` maps to `GIT_TOKEN` kind `git`. Any other name is passed
   through literally with the given `-kind` (default `generic`).

**Acceptance criteria.**
- `POST /api/v1/credentials` with a valid token stores an encrypted credential row for the
  caller's org; the plaintext never appears in the DB (`encrypted_value` is ciphertext).
- `kiwi creds set anthropic sk-...` results in a credential named `ANTHROPIC_API_KEY`.
- Unauthenticated request → 401. Empty name/value → 400.

**Tests.**
- Handler test (`httptest` + in-memory store like other `pkg/orchestrator` tests, or the
  planner test's store helper): valid save → 204 and row present & encrypted; missing claims
  → 401; empty value → 400.
- Client test: `SetCredential` sends the right method/path/body/header (mirror
  `pkg/client/client_test.go`).

**Out of scope.** Listing/rotating/deleting credentials; a UI. (Add a `list` that returns
names only — never values — if trivial, otherwise skip.)

**Gotchas.** Do not log the value. Do not return it. Org-scope strictly.

---

### A2 — Result payload plumbing (carry the PR URL back and store it)

**Priority:** P0. **Depends on:** baseline. **Size:** S–M.

**Goal.** Extend the daemon→CP result path so a completed task can report a **result URL**
(the PR) and a short **detail** string, and persist them on the task row so A4 can surface
them.

**Why.** Today `ResultReq` carries only `{TaskID, LeaseID, Status, SignPubKey}` and
`handleDaemonResult` only calls `CompleteTask`. There is nowhere to put a PR URL.

**Files.**
- `pkg/daemon/types.go` — extend `ResultReq` (single source of truth).
- `pkg/store/queue_models.go` — add columns to `QueuedTask`.
- `pkg/store/queue.go` — extend `CompleteTask` to persist result fields.
- `pkg/orchestrator/daemon_api.go` — `handleDaemonResult` reads + stores the new fields.
- `pkg/daemon/daemon.go` — `reportResult` accepts and sends the new fields.
- `migrations/0003_task_result.up.sql` / `.down.sql` (new) — add the columns.

**Implementation.**
1. `ResultReq` gains: `ResultURL string \`json:"result_url,omitempty"\`` and
   `Detail string \`json:"detail,omitempty"\``.
2. `QueuedTask` gains: `ResultURL *string \`json:"result_url"\`` and
   `ResultDetail *string \`json:"result_detail"\``.
3. Change `CompleteTask(ctx, taskID, leaseID, finalStatus string)` →
   `CompleteTask(ctx, taskID, leaseID, finalStatus, resultURL, detail string)`. In the
   `Updates` map, set `result_url` and `result_detail` (store `nil` when empty strings, to
   keep columns NULL). Update ALL callers (there is a caller in `daemon_api.go` heartbeat
   dead-letter path and in tests — grep `CompleteTask(`).
4. `handleDaemonResult`: decode `req.ResultURL`, `req.Detail`; pass them into `CompleteTask`.
5. `reportResult(ctx, taskID, leaseID string, ok bool, resultURL, detail string)`: include
   them in the signed `ResultReq`. Update its callers in `pollCP`.
6. Migration `0003`: `ALTER TABLE queued_tasks ADD COLUMN result_url TEXT; ADD COLUMN
   result_detail TEXT;` (down: drop them).

**Acceptance criteria.**
- A daemon reporting SUCCEEDED with a `result_url` persists it on the task row; the fencing
  check still holds (wrong `lease_id` → no write, 409).
- Existing behavior unchanged when `result_url`/`detail` are empty.

**Tests.**
- `pkg/store` queue test: `CompleteTask` with a URL persists it; with a stale lease it does
  not; empty URL leaves the column NULL.
- `pkg/orchestrator` daemon_api test: `handleDaemonResult` with a URL body stores it.

**Gotchas.** Keep the fencing semantics (`WHERE id=? AND lease_id=? AND status=LEASED`).
Update every `CompleteTask(` call site or the build breaks.

---

### [DONE] A3 — Daemon delivery: commit → push → open GitHub PR  ★ the visible output

**Priority:** P0. **Depends on:** A1, A2. **Size:** M (largest task).

**Goal.** After the Actor–Critic loop passes and files changed, the daemon commits the
worktree to a per-job branch, pushes it using the org's `GIT_TOKEN`, opens a GitHub PR
against the base ref, and returns the PR URL through A2's result path.

**Why.** Today the daemon runs the loop and reports SUCCEEDED but produces no artifact. This
is the step that turns a green run into something a human can see and merge.

**Files.**
- `pkg/daemon/delivery.go` (new) — all commit/push/PR logic.
- `pkg/daemon/delivery_test.go` (new) — tests.
- `pkg/daemon/daemon.go` — call delivery after `runner.Run` succeeds; thread PR URL into
  `reportResult`.
- `pkg/agent/agent.go` — add `JobID string \`json:"job_id"\`` to `WorkerSpec`.
- `pkg/planner/service.go` — add `"job_id": jobID` to the enqueued spec map (it already has
  `jobID` in scope).

**Implementation.**
1. **Job id on the spec.** Add `JobID` to `WorkerSpec`; set `"job_id"` in the spec built by
   `planner/service.go`. Branch name = `kiwi/<JobID>` (fall back to `kiwi/<spec.ID>` if
   `JobID` empty, for robustness).
2. **New function** in `delivery.go`:
   `func publishResult(ctx context.Context, worktreePath string, spec agent.WorkerSpec, gitToken string, gh githubClient) (prURL string, err error)`.
   Steps, all shelling out to `git` with `exec.CommandContext` and `cmd.Dir = worktreePath`
   (mirror the style in `pkg/sandbox/exec.go`):
   - `git add -A`.
   - Check there is something to commit: `git status --porcelain`; if empty, return
     `("", nil)` (no PR — the loop passed without changes; caller reports SUCCEEDED with
     detail "no changes").
   - `git -c user.email=bot@runkiwi.com -c user.name="Kiwi" commit -m "kiwi: <spec.Task>"`.
   - Push: build an authenticated URL from `spec.RepoURL`. For GitHub HTTPS
     `https://github.com/OWNER/REPO(.git)` → push URL
     `https://x-access-token:<gitToken>@github.com/OWNER/REPO.git`. Run
     `git push <authURL> HEAD:refs/heads/kiwi/<JobID>`. **Never log the authURL** (it
     contains the token) — log the sanitized `spec.RepoURL` only.
   - Open PR via `gh` (the `githubClient` interface, see below): base = `spec.Ref` (default
     `main`), head = `kiwi/<JobID>`, title = `"Kiwi: <spec.Task>"`, body = a short summary.
   - Return the PR's `html_url`.
3. **GitHub client seam (for testability).** Define
   `type githubClient interface { CreatePR(ctx, owner, repo, base, head, title, body string) (htmlURL string, err error) }`.
   Provide a real implementation `type restGitHub struct{ token string; api string }` that
   POSTs to `<api>/repos/{owner}/{repo}/pulls` (default `api = https://api.github.com`) with
   header `Authorization: Bearer <token>`, body `{"title","head","base","body"}`, and parses
   `html_url` from the JSON response. Make `api` overridable so tests point it at
   `httptest.NewServer`.
4. **Owner/repo parsing.** A helper `parseGitHubRepo(repoURL) (owner, repo string, ok bool)`
   handling `https://github.com/OWNER/REPO`, `.../REPO.git`, and rejecting non-GitHub hosts
   (return `ok=false`; caller reports SUCCEEDED-without-PR and detail "unsupported host"). MVP
   is GitHub-only.
5. **Wire into `daemon.go`.** After `result, err := runner.Run(...)` returns success:
   read `gitToken := creds["GIT_TOKEN"]`; if empty, skip push and set detail
   "no GIT_TOKEN; skipped PR". Otherwise call `publishResult`; pass the returned `prURL` and
   a detail into `reportResult(ctx, spec.ID, res.LeaseID, true, prURL, detail)`.

**Acceptance criteria.**
- Given a worktree with a passing loop and changes, the daemon creates commit on branch
  `kiwi/<jobID>`, pushes it, calls the GitHub PR API once, and reports the returned PR URL.
- No changes after the loop → SUCCEEDED, no PR, detail explains it.
- Missing `GIT_TOKEN` → SUCCEEDED, no push, detail explains it (never crash).
- Non-GitHub repo URL → SUCCEEDED, no PR, detail explains it.
- The git token never appears in any log line.

**Tests (no real network / no real GitHub).**
- Git ops against a **local bare repo**: in the test, `git init --bare` a temp dir as
  "origin", clone it to a worktree, write a change, and run `publishResult` with a fake
  `githubClient` and push URL pointing at the local bare repo (use the bare repo path as the
  "remote"; you can special-case the auth URL builder for `file://`/local paths, or inject
  the push remote). Assert the branch exists in the bare repo (`git --git-dir=<bare>
  rev-parse refs/heads/kiwi/<job>` succeeds).
- `parseGitHubRepo`: table test for `.git`, no-`.git`, non-GitHub → not ok.
- `restGitHub.CreatePR`: point `api` at an `httptest.Server` asserting method/path/body and
  returning `{"html_url":"https://github.com/o/r/pull/1"}`; assert the URL is returned.
- Fake `githubClient` records the single call in `publishResult`.

**Out of scope.** Updating an existing PR; GitLab/Bitbucket; conflict handling on the branch;
multi-worker composition (single worker only). Draft PRs. Labels/reviewers.

**Gotchas.** Redact the token in logs (build the auth URL only at the push call site).
`git push` may fail if the branch exists from a prior run — use a deterministic branch and,
on push, allow force-with-lease OR make the branch name include a short attempt suffix; the
simplest correct choice for MVP: `git push <authURL> HEAD:refs/heads/kiwi/<JobID>` and if it
fails because the ref exists, report FAILED with the git error (do not silently overwrite).

--- [DONE] **A4: BYOC job-status endpoint + CLI poll** (show the PR URL)

**Priority:** P0. **Depends on:** A2, A3. **Size:** S.

**Goal.** Let `kiwi submit -repo` poll for completion and print the PR URL.

**Why.** `submitPlan` in `cmd/kiwi/submit.go` currently returns immediately at "enqueued".
There is no BYOC job-status read path.

**Files.**
- `pkg/store/queue.go` — add `GetJobTasks(ctx, orgID, jobID string) ([]QueuedTask, error)`
  (org-scoped, ordered by `created_at`).
- `pkg/orchestrator/jobs_api.go` (new) — `GET /api/v1/jobs/{jobID}` handler (behind auth,
  org-scoped) returning each task's `id`, `status`, `result_url`, `result_detail`.
- `pkg/orchestrator/server.go` — register `mux.HandleFunc("/api/v1/jobs/", s.handleJobStatus)`.
- `pkg/client/client.go` — `GetJob(ctx, jobID) (*JobStatus, error)`.
- `cmd/kiwi/submit.go` — after `PlanTask`, poll `GetJob` until all tasks terminal; print PR
  URL(s). Respect the existing `-interval` flag.

**Implementation.**
1. Endpoint returns `{"job_id":..., "tasks":[{"id","status","result_url","result_detail"}]}`.
   Org-scope by `claims.OrgID`; 404 if no tasks match `(org_id, job_id)`.
2. CLI poll loop: every `-interval`, call `GetJob`; when every task is `SUCCEEDED`/`FAILED`,
   print a summary line per task (`✓ <id> → <result_url>` or `✗ <id> FAILED: <detail>`) and
   return non-zero if any FAILED.

**Acceptance criteria.**
- After a real end-to-end run, `kiwi submit -repo ...` blocks, then prints the PR URL and
  exits 0. If the worker FAILED, it prints the detail and exits non-zero.
- The endpoint never returns another org's tasks.

**Tests.**
- Store: `GetJobTasks` returns only the org's tasks for that job, ordered.
- Handler: seeded job → correct JSON; other org → 404.
- Client: `GetJob` parses the response.

**Gotchas.** Org-scope the query. Do not leak `spec` internals in the status response — only
the four fields above.

---

### [DONE] A5 — Idempotent plan submission

**Priority:** P1. **Depends on:** A1–A4. **Size:** S–M.

**Goal.** Honor the `Idempotency-Key` header on `/api/v1/planner/plan` so a retried submit
returns the **same** job instead of enqueuing duplicate work.

**Why.** The CLI already sends `Idempotency-Key`; the planner ignores it. For SaaS,
network retries must not double-run (and double-bill) tasks.

**Files.**
- `pkg/planner/handler.go` — read the header, pass into the service.
- `pkg/planner/service.go` — dedupe: if a plan for `(org_id, idempotency_key)` exists, return
  it; else create and record it.
- `pkg/store` — a small table `plan_submissions(org_id, idempotency_key UNIQUE per org,
  job_id, created_at)` + migration `0004`.

**Implementation.** On submit with a non-empty key, `INSERT ... ON CONFLICT (org_id,
idempotency_key) DO NOTHING`; if the row already existed, load its `job_id` and return the
prior `SubmitResult` (re-read task ids). Wrap in the same transaction that creates the
manifest/tasks so a partial submit cannot leave a dangling key.

**Acceptance criteria.** Two submits with the same key + org produce **one** job and one set
of tasks; the second returns the first's ids. Different keys → different jobs. No key →
current behavior (always new).

**Tests.** Service test: same key twice → identical `JobID`, task count unchanged in DB.

---

## PHASE B — Deploy as SaaS

### [DONE] B1 — Health & readiness endpoints

**Priority:** P0 (for deploy). **Depends on:** baseline. **Size:** S.

**Goal.** Add `GET /healthz` (process up, always 200) and `GET /readyz` (200 only if the DB
is reachable — `s.db` ping) so a load balancer / orchestrator can gate traffic.

**Files.** `pkg/orchestrator/server.go` (register on `root`, **before** `AuthMiddleware`, so
health checks need no token). Add a `sqlDB, _ := s.db.DB(); sqlDB.PingContext(ctx)` for
`/readyz`.

**Acceptance criteria.** `/healthz` returns 200 without auth; `/readyz` returns 200 when
Postgres is up, 503 when it is not. Neither requires a token.

**Tests.** Handler tests for both, including `/readyz` 503 with a closed DB.

---

### B2 — Migrations on boot & schema reconciliation

**Priority:** P0 (for deploy). **Depends on:** baseline (and merges A2/A5 migrations).
**Size:** M.

**Goal.** The Control Plane must converge the DB schema on startup. Today `migrations/` has
`0001`/`0002`, but `queued_tasks` and `credentials` exist only via ad-hoc `AutoMigrate`
(schema drift). Make one authoritative path.

**Files.**
- `migrations/` — add any missing tables to numbered SQL (reconcile `queued_tasks`,
  `credentials`, `daemons`, `daemon_join_tokens`, `manifests`, plus new `0003`/`0004`).
- `cmd/kiwid/main.go` — run migrations on boot (embed the `migrations/` dir with
  `//go:embed` and apply with a lightweight runner, or call the existing migrate path if one
  exists — **grep first**). Keep `AutoMigrate` only as a dev fallback gated behind an env
  flag `KIWI_AUTOMIGRATE=true`; production uses SQL migrations.

**Implementation.** Prefer a simple embedded-SQL forward-only runner (apply any `*.up.sql`
not yet recorded in a `schema_migrations` table). Do NOT add a heavy migration framework.
Verify the final schema matches the GORM models (`pkg/store/*_models.go`, `pkg/auth/models.go`).

**Acceptance criteria.** Booting `kiwid` against an empty Postgres produces a schema that all
package tests and the end-to-end flow run against, with no reliance on `AutoMigrate` in
production. Re-running is a no-op (idempotent).

**Tests.** A migration-runner unit test against a temp DB (SQLite is used elsewhere in tests;
if the runner is Postgres-specific, gate the test with a `KIWI_TEST_PG_DSN` env and skip when
unset). Document the drift you reconciled in the PR.

**Gotchas.** This is the riskiest deploy task — call out every table you added to SQL in the
PR so the reviewer can diff against the models.

---

### B3 — Production config: env-driven, fail-fast on missing secrets

**Priority:** P0 (for deploy). **Depends on:** baseline. **Size:** S–M.

**Goal.** Make `kiwid` configurable entirely by environment (12-factor) and refuse to start
insecurely.

**Files.** `cmd/kiwid/main.go`, and a small `pkg/orchestrator` config helper if useful.

**Implementation.**
- Read from env with flag fallback: `KIWI_ADDR`, `KIWI_DSN`, `KIWI_ROLE`, `KIWI_NATS_URL`.
- **Fail fast** at startup if, in production (`KIWI_ENV=production`):
  - `KIWI_ENCRYPTION_KEY` is missing/invalid (must be 32-byte hex) — without it, at-rest
    encryption silently uses a dev key. Refuse to boot.
  - `KIWI_SERVER_TOKEN` is unset (used by admin/bootstrap).
  - `KIWI_CORS_ALLOWED_ORIGINS` is `*` or unset (do not allow wildcard CORS in prod).
- Log the resolved, **non-secret** config at boot (never log secrets).

**Acceptance criteria.** In `KIWI_ENV=production`, missing/weak `KIWI_ENCRYPTION_KEY`,
missing `KIWI_SERVER_TOKEN`, or wildcard CORS causes a clear fatal error at startup. In dev
(default) behavior is unchanged.

**Tests.** A config-validation unit test covering each fail-fast condition.

---

### B4 — Deployment artifacts (single-VM Docker Compose + TLS, incl. one managed daemon)

**Priority:** P0 (for deploy). **Depends on:** B1, B2, B3. **Size:** M.

**Goal.** A concrete, minimal way to run the whole SaaS on one host with automatic HTTPS,
plus one Kiwi-operated daemon so the platform is end-to-end usable immediately (managed-tier
v1: Kiwi runs the daemon; this is **not** zero-knowledge — do not label it so).

**Files (all under `deploy/`, new):**
- `deploy/docker-compose.prod.yml` — services: `postgres` (with a named volume),
  `kiwid` (build from repo, `KIWI_ENV=production`, all env from an env file), `caddy`
  (reverse proxy terminating TLS to `kiwid:8080`), and `kiwidaemon` (build from repo, runs
  `cmd/kiwidaemon`, mounts a volume for its identity key, gets `-api-url` = internal `kiwid`
  and a `KIWI_JOIN_TOKEN` from env).
- `deploy/Caddyfile` — `your-domain { reverse_proxy kiwid:8080 }` (auto-HTTPS via Let's
  Encrypt).
- `deploy/.env.example` — every required variable with a comment (KIWI_ENCRYPTION_KEY,
  KIWI_SERVER_TOKEN, DSN parts, KIWI_CORS_ALLOWED_ORIGINS, KIWI_JOIN_TOKEN, domain).
- `deploy/README.md` — a step-by-step runbook: provision a VM, set env, `docker compose -f
  deploy/docker-compose.prod.yml up -d`, create the first org + API key (see B5), mint a join
  token, verify `/readyz`, run a sample `kiwi submit -repo ...`.
- Extend the root `Dockerfile` (or add `Dockerfile.daemon`) so `cmd/kiwidaemon` also builds.

**Acceptance criteria.** `docker compose -f deploy/docker-compose.prod.yml up` on a clean VM
brings up Postgres + kiwid (migrated, `/readyz` green) behind HTTPS, plus a registered daemon
that heartbeats. The runbook takes a new operator from zero to a successful `kiwi submit`.

**Tests.** N/A (infra) — but `docker compose config` must validate, and the README steps must
be accurate. Include a `make deploy-validate` target that runs `docker compose -f
deploy/docker-compose.prod.yml config -q`.

**Gotchas.** Do not commit real secrets — only `.env.example`. Alternative target (Fly.io):
add a short "Alternative: Fly.io" section with a `fly.toml` if time permits, but the Compose
path is the deliverable.

---

### B5 — Onboarding/bootstrap: first org + API key, and admin auth

**Priority:** P0 (for deploy). **Depends on:** baseline. **Size:** S–M.

**Goal.** A safe way to create the first org and issue an API key on a fresh deployment, and
protect the `/admin/*` surface.

**Why.** `pkg/auth/admin.go` exposes `/admin/orgs` and API-key creation, but for SaaS these
must require an admin secret, and there must be a one-command bootstrap.

**Files.** `pkg/auth/admin.go` (gate `/admin/*` behind `KIWI_SERVER_TOKEN` via a header check
if not already), `cmd/kiwid` or a small `cmd/kiwi-admin` (or a `make bootstrap` target /
script `deploy/bootstrap.sh`) that: creates an org, a user, and an API key, printing the key
once.

**Implementation.**
- Ensure every `/admin/*` route requires `Authorization: Bearer $KIWI_SERVER_TOKEN` (or an
  equivalent admin check). **Grep** `admin.go` first — if a check exists, harden it; do not
  duplicate.
- Provide `deploy/bootstrap.sh` that curls `/admin/orgs` then the key-creation endpoint using
  `$KIWI_SERVER_TOKEN`, and prints the resulting org id + API key.

**Acceptance criteria.** On a fresh deploy, running the bootstrap with the admin token yields
a usable org id + API key; `/admin/*` returns 401 without the admin token.

**Tests.** Handler test: `/admin/orgs` without the admin token → 401; with it → creates org.

**Gotchas.** Print the API key exactly once; never store its plaintext (the store already
hashes it — verify). Do not weaken existing auth.

---

## PHASE C — Hardening (needed for real SaaS, after the demo works)

### C1 — Daemon lease renewal for long tasks (issue #121)

**Priority:** P1. **Depends on:** baseline. **Size:** S–M.
**Goal.** The daemon must call `RenewLease` (already in `pkg/store/queue.go` and reachable via
a daemon endpoint if wired) on a timer while a task runs, so a long Actor–Critic loop does not
have its lease expire and its task requeued underneath it. Add a heartbeat-driven renew from
`pkg/daemon` and a CP endpoint if missing. **Acceptance:** a task that runs longer than the
lease TTL is not requeued while its daemon is alive. **Tests:** store-level renew already
tested; add a daemon-side timer test with a fake CP.

### C2 — Enforce concurrency + budget caps on the lease path (parallelism precondition)

**Priority:** P1. **Depends on:** baseline. **Size:** M.
**Goal.** `OrgLimits.MaxConcurrentJobs` (default 10) is defined but **not enforced**. In
`LeaseNextTask`, refuse to lease beyond the org's in-flight (`LEASED`) cap; enforce a per-job
budget cap using the loop's `CostUSD`. **Why:** fire-and-forget at scale must be safe before
it is fast — this is the precondition for the massive-parallelism roadmap. **Acceptance:** an
org at its cap gets no new lease until one completes; a job exceeding its budget is stopped.
**Tests:** lease-path test asserting the cap holds; budget test.

### C3 — Enforce per-worker file scope / fix `spec.File` path traversal (issue #131)

**Priority:** P1. **Depends on:** A3. **Size:** S.
**Goal.** A worker must only write within its worktree and (ideally) only its scoped file.
`filepath.Join(worktreePath, spec.File)` with a `../` `spec.File` escapes the worktree.
Validate `spec.File` is a clean relative path inside the worktree before use. **Acceptance:**
a `spec.File` containing `..` or an absolute path is rejected; normal paths work. **Tests:**
table test of malicious vs benign paths.

### C4 — Failed-dependency cascade

**Priority:** P2. **Depends on:** #135 (DAG enforcement). **Size:** S.
**Goal.** With DAG enforcement, a task whose dependency `FAILED` stays `QUEUED` forever (its
dep can never become SUCCEEDED). Add job-level failure propagation: when a task FAILS, mark its
still-QUEUED dependents FAILED (or the whole job FAILED) so nothing hangs. **Acceptance:** a
failed dependency terminates dependents deterministically; the job reaches a terminal state.
**Tests:** queue test: dep FAILED → dependents FAILED, not stuck QUEUED.

---

## Suggested execution order

**Make it work:** A1 → A2 → A3 → A4 → A5.
**Make it deployable:** B1 → B3 → B2 → B5 → B4.
**Make it safe at scale:** C2 → C1 → C3 → C4.

A1–A4 + B1–B5 + B4 is the minimum for a hosted, demoable SaaS that produces a real PR
end-to-end. Everything in Phase C is required before inviting real concurrent load.
