# Prod CI/CD design

Status: approved, not yet implemented.
Date: 2026-08-10.

## Problem

Kiwi has no CD. `.github/workflows/ci.yml` and `frontend-ci.yml` build and test
only. Every deploy to the real production environment (GCP project
`kiwi-prod-502913`) is manual: SSH/`gcloud` by hand, following the runbook
steps encoded in `deploy/README.md`, `deploy/gcp/control-plane/README.md`, and
tribal knowledge about ordering. This has already caused a real incident class
(the `kiwidaemon:latest` tag on the free-fleet VM silently drifting behind
because a deploy touched some components but not others).

This design covers automating deploys to that one real production
environment whenever code merges to `main`. It explicitly does **not** cover:

- `deploy/docker-compose.prod.yml` — the self-host single-VM runbook. Each
  self-hoster manages their own deploy; there's nothing for Kiwi's CI to
  automate there.
- `deploy/gcp/daemon-vm` — the dedicated-tier Firecracker path. Per
  `CLAUDE.md`, this is "in progress... not deployed or hardware-validated."
  Automating deploys to a path that isn't live yet is out of scope.

## Current manual topology (what we're automating)

GCP project `kiwi-prod-502913`, region `us-central1`:

- Cloud Run services: `kiwi-api`, `kiwi-orchestrator` (singleton, min=max=1),
  `kiwi-frontend`.
- Cloud Run job `kiwi-migrate` (runs `kiwid -role migrate`).
- Artifact Registry repo `kiwi-repo` holding `kiwid`, `kiwidaemon`, `frontend`
  images.
- Free-fleet VM `kiwi-free-fleet` (private IP only), running
  `kiwi-provisioner.service` and `kiwi-daemon-image.service` /
  `kiwi-daemon-image.timer`, which pull `kiwidaemon:latest` and cold-start
  per-org daemon containers on task submit.

Existing manual runbook order (source: `deploy/README.md`,
`deploy/gcp/control-plane/README.md`, and prior verified prod notes):
migrations first (written backward-compatible on purpose) → `kiwi-api` →
`kiwi-orchestrator` → `kiwi-frontend` → fleet VM systemd restart to pick up
new `kiwidaemon:latest`.

## Approach

Plain GitHub Actions + `gcloud` CLI, in a new `.github/workflows/deploy.yml`.
Considered and rejected:

- **Folding deploys into the existing Terraform** (`deploy/gcp/control-plane`).
  Terraform is provisioning-shaped (occasional, stateful, needs a remote-state
  backend + locking to run safely from CI); deploys are frequent,
  single-value image-tag bumps. Fighting Terraform's drift detection on every
  merge is more complexity than it buys.
- **A Go `opsctl deploy` subcommand** encapsulating the gcloud orchestration,
  usable from both CI and by a human during an incident. Genuinely nice
  property (no duplicated logic between "what CI does" and "what you'd type
  by hand at 2am"), but premature to build before the plain-YAML pipeline has
  proven its exact shape. Worth revisiting once `deploy.yml` is stable.

## Pipeline

New workflow `.github/workflows/deploy.yml`.

- **Triggers:** `push` to `main`, and `workflow_dispatch` (for manual re-runs
  — validating the pipeline itself, or retrying a flaky fleet-VM step without
  a new commit).
- **Scope:** every merge deploys *all* components (migrations + all three
  Cloud Run services + the fleet VM daemon image), not path-filtered. This
  trades some pipeline speed for closing the drift-bug class described above
  — one pipeline, one consistent state.
- **Jobs run sequentially**, each gating the next via `needs:`:

```
verify → build-and-push → migrate → deploy-api → deploy-orchestrator → deploy-frontend → refresh-fleet
```

### `verify`

Re-run of the existing PR-gate checks (Docker builds alone don't run tests or
lint):
- `go vet ./...`
- `gofmt -l` check (must print nothing)
- `CGO_ENABLED=0 go test ./pkg/...`
- `frontend/`: `npm ci && npm run lint && npm run build`

### `build-and-push`

Builds and pushes to Artifact Registry `kiwi-repo`, all
`--platform linux/amd64`:
- `kiwid:<git-sha>`
- `kiwidaemon:<git-sha>` **and** `kiwidaemon:latest` (the fleet VM's
  provisioner tracks the floating tag)
- `frontend:<git-sha>`, built with:
  - `--build-arg NEXT_PUBLIC_KIWI_API_URL=https://api.runkiwi.dev`
  - `--build-arg NEXT_PUBLIC_POSTHOG_KEY=${{ secrets.POSTHOG_KEY }}`
  - `--build-arg NEXT_PUBLIC_POSTHOG_HOST=...`
  - The workflow fails explicitly if `POSTHOG_KEY` resolves empty, closing
    the known trap where this arg fails *silently* and ships a dashboard that
    has quietly stopped recording analytics.

### `migrate`

Updates the `kiwi-migrate` Cloud Run job to the new SHA image, executes it,
waits for completion, fails the pipeline on nonzero exit. Runs before any
service deploy, matching the "migrations are backward-compatible by design,
always go first" constraint — old code against new schema is always a safe
intermediate state.

### `deploy-api` / `deploy-orchestrator` / `deploy-frontend`

Same canary pattern for each, run in that order:

1. Capture the currently-serving revision name (for rollback).
2. `gcloud run deploy --image <img>:<sha> --no-traffic --tag=canary-<sha>` —
   creates the revision without cutting traffic to it.
3. Poll the canary's unique per-revision URL — `/healthz` then `/readyz` —
   with retries/backoff to absorb cold start.
4. On success: `gcloud run services update-traffic --to-latest`, then
   re-verify the *public* URL once traffic is live.
5. On failure at step 3 or 4: `update-traffic` back to the revision captured
   in step 1, fail the job loudly.

### `refresh-fleet`

SSH to `kiwi-free-fleet` via IAP tunnel, using the deploy identity:
1. `sudo systemctl start kiwi-daemon-image.service` (pulls `kiwidaemon:latest`)
2. `sudo systemctl restart kiwi-provisioner.service`
3. Verify both units are `active` and the pulled image digest matches what
   was just pushed.

No auto-rollback here — unlike Cloud Run, there's no traffic-split primitive
on a systemd/docker-pull flow. Blast radius is naturally capped since
already-running per-org daemon containers aren't touched, only newly
cold-started ones would pick up a bad image. On verification failure: fail
the workflow and alert; a human intervenes (same "alert and stop" fallback
used for anything without a clean rollback primitive).

## GCP auth: Workload Identity Federation

No long-lived service account key. One-time setup (run by a human against
the real `kiwi-prod-502913` project — out of scope for this agent to execute):

- WIF pool + provider trusting `token.actions.githubusercontent.com`,
  restricted to this repo and to `ref:refs/heads/main` (so a PR from a fork
  can never mint prod credentials).
- Dedicated deploy service account, e.g.
  `kiwi-deploy@kiwi-prod-502913.iam.gserviceaccount.com` — deliberately
  **not** the same identity as `kiwi-cloudrun-sa` (the runtime identity the
  services run as). Separating "what CI can do to deploy" from "what the
  running service can do at runtime" is the least-privilege boundary that
  matters most here. Grants:
  - `roles/run.developer` on the three Cloud Run services + the migrate job
    (resource-scoped, not project-wide)
  - `roles/artifactregistry.writer` on `kiwi-repo`
  - `roles/iap.tunnelResourceAccessor` + an OS Login role, scoped to the
    `kiwi-free-fleet` instance only
  - `roles/iam.workloadIdentityUser` binding for the WIF pool
  - Explicitly **not** `roles/editor` or anything project-wide
- Delivered as `deploy/gcp/bootstrap-cicd.sh`, a script containing the exact
  `gcloud` commands, for a human to run once against the real project.

## New GitHub repo configuration required

Secrets:
- `GCP_WORKLOAD_IDENTITY_PROVIDER`
- `GCP_DEPLOY_SA_EMAIL`
- `POSTHOG_KEY` (currently only in gitignored `frontend/.env.local`)

No SA JSON key secret — that's the point of WIF.

## Rollback story

- **Cloud Run** (api/orchestrator/frontend): automatic on health-check
  failure, per the canary steps above. A human can also always manually
  `gcloud run services update-traffic` to any older revision.
- **Migrations**: intentionally **not** auto-rolled-back. Forward-only by
  design — a bad migration is fixed with a new forward migration, not an
  automatic undo. The workflow's failure output states this explicitly so
  nobody expects otherwise mid-incident.
- **Fleet VM**: no auto-rollback, alert-and-stop, for the reasons above.

## Validating the pipeline itself

`workflow_dispatch` allows triggering the full pipeline on demand before
trusting it unattended. Recommended: one manually-triggered run, watched
closely end-to-end, before treating subsequent merges as fire-and-forget.

## File layout

- `.github/workflows/deploy.yml` — the pipeline described above.
- `deploy/gcp/bootstrap-cicd.sh` — one-time WIF/SA/IAM setup script (human-run).
- `deploy/README.md` — updated: the manual steps become "how the automated
  pipeline works," with a trimmed emergency/manual-override section retained
  for incident use, not as the primary path.

## Out of scope for this design

- Deploying `deploy/docker-compose.prod.yml` (self-host) or
  `deploy/gcp/daemon-vm` (not yet live) targets.
- A reusable `opsctl deploy` CLI (noted above as a good future step).
- Slack/Discord failure notifications — GitHub's native failure notifications
  (e.g. email) are the v1 mechanism; nothing requested beyond that.
