# Kiwi Managed Tier: Control Plane

This Terraform module provisions the Kiwi Control Plane on Google Cloud Platform. It sets up the trusted "control zone" resources required to manage tasks and agents.

## Architecture

*   **Cloud SQL (Postgres):** Highly available, private IP database with automated backups and PITR.
*   **Artifact Registry:** Docker repository for Kiwi images (`kiwid`, `frontend`).
*   **Secret Manager:** Stores sensitive bootstrap tokens and provider keys.
*   **Cloud KMS:** Keyring and crypto key for envelope encryption of database secrets.
*   **Cloud Run Services:**
    *   `kiwi-api`: Stateless API service that scales horizontally.
    *   `kiwi-orchestrator`: Singleton (`min=max=1`) orchestrator service for background sweeping.
    *   `kiwi-frontend`: Web application interface.
*   **Cloud Run Job (`kiwi-migrate`):** A run-once job to apply database migrations before deploying new `kiwi-api` revisions.
*   **Cloud Load Balancing:** Routes traffic to the API and frontend with a Google-managed TLS certificate.
*   **VPC and Subnets:**
    *   A Serverless VPC Access connector for Cloud Run to reach the private Cloud SQL instance.
    *   A dedicated `daemon` subnet for the execution zone (used in Phase G3).

## Prerequisites

1.  A Google Cloud Project with billing enabled.
2.  Terraform >= 1.5.0 installed.
3.  `gcloud` CLI installed and authenticated (`gcloud auth application-default login`).
4.  Required APIs enabled:
    *   `compute.googleapis.com`
    *   `run.googleapis.com`
    *   `sqladmin.googleapis.com`
    *   `vpcaccess.googleapis.com`
    *   `servicenetworking.googleapis.com`
    *   `secretmanager.googleapis.com`
    *   `cloudkms.googleapis.com`
    *   `artifactregistry.googleapis.com`

## Runbook: Deployment

### 1. Build and Push Images

Before deploying the Cloud Run services, build and push the Docker images to the Artifact Registry repo. (Note: The first time, you may need to apply the Artifact Registry portion of the Terraform code alone, or just create it manually).

```bash
# Build API and Orchestrator image
docker build -t us-central1-docker.pkg.dev/YOUR_PROJECT_ID/kiwi-repo/kiwid:latest --target kiwid -f Dockerfile .
docker push us-central1-docker.pkg.dev/YOUR_PROJECT_ID/kiwi-repo/kiwid:latest

# Build Frontend image
docker build -t us-central1-docker.pkg.dev/YOUR_PROJECT_ID/kiwi-repo/frontend:latest -f frontend/Dockerfile frontend
docker push us-central1-docker.pkg.dev/YOUR_PROJECT_ID/kiwi-repo/frontend:latest
```

### 2. Configure Terraform

Copy the example variables file and fill in your specific values:

```bash
cp terraform.tfvars.example terraform.tfvars
# Edit terraform.tfvars
```

### 3. Initialize and Apply Terraform

```bash
terraform init
terraform apply -var-file=terraform.tfvars
```

### 4. Run Migrations

Before routing traffic to a newly deployed API, run the migration job:

```bash
gcloud beta run jobs execute kiwi-migrate --region us-central1
```

Wait for the job to complete successfully.

### 5. Verify Deployment

*   Check that the Load Balancer is provisioning the SSL certificate (this can take 15-30 minutes).
*   Verify the `/healthz` and `/readyz` endpoints on the API domain.
*   Access the frontend domain to ensure the UI loads.

## Continuous Deployment

Step 1 (build and push images) and step 4 (run migrations) of the runbook
above, plus the Cloud Run service updates step 3's `terraform apply` would
otherwise be used for, are automated by
[`.github/workflows/deploy.yml`](../../../.github/workflows/deploy.yml) on
every merge to `main`. It builds and pushes `kiwid`, `kiwidaemon`, and
`frontend`, runs `kiwi-migrate`, then deploys `kiwi-api` and
`kiwi-frontend` each as a canary revision: deployed with `--no-traffic`,
health-checked on its own URL, promoted to 100% traffic, re-verified, and
automatically rolled back to the previous revision if the post-cutover
check fails (the pre-cutover canary check simply leaves prod traffic
untouched, since it never moved). `kiwi-orchestrator` is deployed
differently — see below. It finishes by refreshing the
free-fleet VM's `kiwidaemon:latest`, starting `kiwi-daemon-image.service` and
restarting `kiwi-provisioner.service` — waking the VM first via
`gcloud compute instances start` if it was idle and autoscaled to zero.
`kiwi-orchestrator` is deployed directly rather than through the canary
pattern above — it has no public HTTP surface to health-check and is a
singleton where two concurrent instances is unsafe, so `gcloud run deploy`'s
own built-in readiness gate is used instead.

This Terraform module still owns *provisioning* — creating the services,
networking, and IAM the first time, or changing their shape (env vars,
scaling, secrets). Routine version bumps go through `deploy.yml`, not
`terraform apply`. Note that after `deploy.yml` runs, the live Cloud Run
image tags will differ from whatever is in `terraform.tfvars` — an unrelated
`terraform apply` (e.g. to add an env var) will read the stale tag from
tfvars and can silently roll a service back to it, so keep
`terraform.tfvars`'s image references current or expect to re-run
`deploy.yml` afterward.

One-time setup for the pipeline's GCP credentials (Workload Identity
Federation, a scoped deploy service account) lives in
[`deploy/gcp/bootstrap-cicd.sh`](../bootstrap-cicd.sh); run it once, then set
the `GCP_WORKLOAD_IDENTITY_PROVIDER` and `GCP_DEPLOY_SA_EMAIL` GitHub Actions
repo secrets it prints, plus `POSTHOG_KEY` (from `frontend/.env.local`) —
the script reminds you to set this one but does not compute or print a
value for it. `POSTHOG_HOST` (a repo *variable*, not a secret) must also be
set — see the script's closing output for where.

**Manual override:** the steps in the runbook above still work for an
emergency out-of-band deploy — `deploy.yml` is a wrapper around the same
`gcloud` commands, not a replacement for knowing them.
