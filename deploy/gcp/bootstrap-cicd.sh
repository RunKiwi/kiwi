#!/usr/bin/env bash
# One-time GCP setup for the deploy.yml CI/CD pipeline: creates a Workload
# Identity Federation pool/provider trusting GitHub Actions, a dedicated
# deploy service account (separate from the kiwi-cloudrun-sa runtime
# identity), and grants it exactly the permissions deploy.yml needs. Run
# once, by a human with sufficient IAM in the real project. Not invoked by
# CI. See docs/superpowers/specs/2026-08-10-prod-cicd-design.md.
set -euo pipefail

PROJECT_ID="${PROJECT_ID:-kiwi-prod-502913}"
REGION="${REGION:-us-central1}"
GITHUB_REPO="${GITHUB_REPO:?set GITHUB_REPO to e.g. RunKiwi/kiwi}"
POOL_ID="${POOL_ID:-github-deploy-pool}"
PROVIDER_ID="${PROVIDER_ID:-github-deploy-provider}"
DEPLOY_SA_NAME="${DEPLOY_SA_NAME:-kiwi-deploy}"
DEPLOY_SA_EMAIL="${DEPLOY_SA_NAME}@${PROJECT_ID}.iam.gserviceaccount.com"
FLEET_VM_NAME="${FLEET_VM_NAME:-kiwi-free-fleet}"
FLEET_VM_ZONE="${FLEET_VM_ZONE:-us-central1-a}"
ARTIFACT_REPO="${ARTIFACT_REPO:-kiwi-repo}"

echo "==> Enabling required APIs"
gcloud services enable iamcredentials.googleapis.com sts.googleapis.com \
  --project "$PROJECT_ID"

echo "==> Creating deploy service account (if it doesn't already exist)"
if ! gcloud iam service-accounts describe "$DEPLOY_SA_EMAIL" --project "$PROJECT_ID" >/dev/null 2>&1; then
  gcloud iam service-accounts create "$DEPLOY_SA_NAME" \
    --project "$PROJECT_ID" \
    --display-name "Kiwi CI/CD deploy identity (GitHub Actions)"
fi

echo "==> Creating Workload Identity Pool"
if ! gcloud iam workload-identity-pools describe "$POOL_ID" --project "$PROJECT_ID" --location global >/dev/null 2>&1; then
  gcloud iam workload-identity-pools create "$POOL_ID" \
    --project "$PROJECT_ID" --location global \
    --display-name "GitHub Actions deploy pool"
fi

echo "==> Creating Workload Identity Provider, restricted to ${GITHUB_REPO} on main"
if ! gcloud iam workload-identity-pools providers describe "$PROVIDER_ID" \
    --project "$PROJECT_ID" --location global --workload-identity-pool "$POOL_ID" >/dev/null 2>&1; then
  gcloud iam workload-identity-pools providers create-oidc "$PROVIDER_ID" \
    --project "$PROJECT_ID" --location global --workload-identity-pool "$POOL_ID" \
    --display-name "GitHub Actions OIDC" \
    --attribute-mapping "google.subject=assertion.sub,attribute.repository=assertion.repository,attribute.ref=assertion.ref" \
    --attribute-condition "assertion.repository == '${GITHUB_REPO}' && assertion.ref == 'refs/heads/main'" \
    --issuer-uri "https://token.actions.githubusercontent.com"
fi

POOL_NUMBER=$(gcloud iam workload-identity-pools describe "$POOL_ID" \
  --project "$PROJECT_ID" --location global --format='value(name)')

echo "==> Allowing the pool to impersonate the deploy service account"
gcloud iam service-accounts add-iam-policy-binding "$DEPLOY_SA_EMAIL" \
  --project "$PROJECT_ID" \
  --role "roles/iam.workloadIdentityUser" \
  --member "principalSet://iam.googleapis.com/${POOL_NUMBER}/attribute.repository/${GITHUB_REPO}"

echo "==> Allowing the deploy identity to act as the Cloud Run runtime service account (required for gcloud run deploy / gcloud run jobs update)"
gcloud iam service-accounts add-iam-policy-binding "kiwi-cloudrun-sa@${PROJECT_ID}.iam.gserviceaccount.com" \
  --project "$PROJECT_ID" \
  --role "roles/iam.serviceAccountUser" \
  --member "serviceAccount:${DEPLOY_SA_EMAIL}"

# kiwi-frontend's Terraform config sets no explicit `service_account`,
# unlike kiwi-api/kiwi-orchestrator/kiwi-migrate (which all run as
# kiwi-cloudrun-sa, granted above) — so it runs as the project's default
# compute service account instead, and `gcloud run deploy kiwi-frontend`
# needs actAs on that identity too.
PROJECT_NUMBER=$(gcloud projects describe "$PROJECT_ID" --format='value(projectNumber)')
DEFAULT_COMPUTE_SA="${PROJECT_NUMBER}-compute@developer.gserviceaccount.com"
echo "==> Allowing the deploy identity to act as kiwi-frontend's runtime service account (${DEFAULT_COMPUTE_SA})"
gcloud iam service-accounts add-iam-policy-binding "$DEFAULT_COMPUTE_SA" \
  --project "$PROJECT_ID" \
  --role "roles/iam.serviceAccountUser" \
  --member "serviceAccount:${DEPLOY_SA_EMAIL}"

echo "==> Granting Cloud Run deploy permissions"
for SERVICE in kiwi-api kiwi-orchestrator kiwi-frontend; do
  gcloud run services add-iam-policy-binding "$SERVICE" \
    --project "$PROJECT_ID" --region "$REGION" \
    --member "serviceAccount:${DEPLOY_SA_EMAIL}" \
    --role "roles/run.developer"
done

echo "==> Granting Cloud Run job (migrate) permissions"
gcloud run jobs add-iam-policy-binding kiwi-migrate \
  --project "$PROJECT_ID" --region "$REGION" \
  --member "serviceAccount:${DEPLOY_SA_EMAIL}" \
  --role "roles/run.developer"

echo "==> Granting Artifact Registry push permissions"
gcloud artifacts repositories add-iam-policy-binding "$ARTIFACT_REPO" \
  --project "$PROJECT_ID" --location "$REGION" \
  --member "serviceAccount:${DEPLOY_SA_EMAIL}" \
  --role "roles/artifactregistry.writer"

echo "==> Granting IAP tunnel + OS Login access, scoped to the fleet VM only"
gcloud compute instances add-iam-policy-binding "$FLEET_VM_NAME" \
  --project "$PROJECT_ID" --zone "$FLEET_VM_ZONE" \
  --member "serviceAccount:${DEPLOY_SA_EMAIL}" \
  --role "roles/iap.tunnelResourceAccessor"

gcloud compute instances add-iam-policy-binding "$FLEET_VM_NAME" \
  --project "$PROJECT_ID" --zone "$FLEET_VM_ZONE" \
  --member "serviceAccount:${DEPLOY_SA_EMAIL}" \
  --role "roles/compute.osAdminLogin"

# roles/compute.viewer's permission set is mostly project-scoped
# (compute.projects.get, compute.zones.list, ...), which Compute Engine
# rejects when bound at a single-instance resource scope — unlike
# iap.tunnelResourceAccessor/osAdminLogin above, which are genuinely
# instance-scoped roles. Bind it at the project level instead; it's
# read-only, so this is a small, deliberate widening, not a write grant.
echo "==> Granting project-level read access to Compute Engine (needed for gcloud compute ssh/describe to resolve the instance and project OS Login metadata)"
gcloud projects add-iam-policy-binding "$PROJECT_ID" \
  --member "serviceAccount:${DEPLOY_SA_EMAIL}" \
  --role "roles/compute.viewer" \
  --condition=None

FLEET_WAKE_ROLE_ID="kiwiDeployFleetWake"
echo "==> Creating (if needed) a minimal custom role granting only compute.instances.start, for waking the fleet VM when it's autoscaled to zero"
if ! gcloud iam roles describe "$FLEET_WAKE_ROLE_ID" --project "$PROJECT_ID" >/dev/null 2>&1; then
  gcloud iam roles create "$FLEET_WAKE_ROLE_ID" \
    --project "$PROJECT_ID" \
    --title "Kiwi Deploy: Fleet VM Wake" \
    --description "Allows starting the free-fleet VM only; used by deploy.yml's refresh-fleet job" \
    --permissions "compute.instances.start" \
    --stage GA
fi

gcloud compute instances add-iam-policy-binding "$FLEET_VM_NAME" \
  --project "$PROJECT_ID" --zone "$FLEET_VM_ZONE" \
  --member "serviceAccount:${DEPLOY_SA_EMAIL}" \
  --role "projects/${PROJECT_ID}/roles/${FLEET_WAKE_ROLE_ID}"

echo
echo "==> Done. Add these as GitHub Actions repo secrets:"
echo "GCP_WORKLOAD_IDENTITY_PROVIDER=${POOL_NUMBER}/providers/${PROVIDER_ID}"
echo "GCP_DEPLOY_SA_EMAIL=${DEPLOY_SA_EMAIL}"
echo
echo "Also set as GitHub Actions repo configuration:"
echo "  - POSTHOG_KEY (secret, from frontend/.env.local)"
echo "  - POSTHOG_HOST (variable, not secret — Settings > Secrets and variables > Actions > Variables)"
