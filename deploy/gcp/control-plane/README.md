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
docker build -t us-central1-docker.pkg.dev/YOUR_PROJECT_ID/kiwi-repo/kiwid:latest -f Dockerfile.kiwid .
docker push us-central1-docker.pkg.dev/YOUR_PROJECT_ID/kiwi-repo/kiwid:latest

# Build Frontend image
docker build -t us-central1-docker.pkg.dev/YOUR_PROJECT_ID/kiwi-repo/frontend:latest -f Dockerfile.frontend .
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
gcloud run jobs execute kiwi-migrate --region us-central1
```

Wait for the job to complete successfully.

### 5. Verify Deployment

*   Check that the Load Balancer is provisioning the SSL certificate (this can take 15-30 minutes).
*   Verify the `/healthz` and `/readyz` endpoints on the API domain.
*   Access the frontend domain to ensure the UI loads.
