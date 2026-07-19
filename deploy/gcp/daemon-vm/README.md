# Kiwi Managed Tier: Daemon VM

This Terraform module provisions a dedicated, per-org execution daemon (the Data Plane) in the Kiwi Managed GCP environment.

## Architecture

*   **Hostile-by-Default:** The daemon VM has no ambient GCP credentials and operates in a dedicated subnet without public IPs.
*   **Nested Virtualization:** Enabled at the hardware level, allowing Firecracker microVMs to run securely within this instance.
*   **Persistent Cache:** A dedicated Google Persistent Disk is attached and mounted at `/mnt/kiwi-cache` for the daemon to persist its `gitcache` across restarts.
*   **Metadata Injection:** The join token and org ID are provided to the instance via instance metadata, avoiding baked-in secrets.
*   **Startup Script:** Automatically formats the cache disk on first boot, installs Docker, and runs the `kiwidaemon` container connected to the Control Plane API.

## Runbook: Provisioning a Daemon

Daemons are hand-provisioned (one per organization) using the `opsctl` CLI. Autoscaling is not used in v1.

### 1. Mint a Join Token

Use the `opsctl` tool to mint a token and generate the Terraform variables:

```bash
opsctl provision-daemon -org-id <org-id> -api-url https://api.runkiwi.com -image us-central1-docker.pkg.dev/.../kiwidaemon:latest
```

This command will output a `terraform.tfvars` file for you to use.

### 2. Apply Terraform

In this directory:

```bash
terraform init
terraform apply -var-file=terraform.tfvars
```

### 3. Verification

*   Check the API dashboard to verify the daemon registered successfully.
*   Check GCP Cloud Logging (or SSH into the VM if enabled for debugging) and run `docker logs kiwidaemon` to ensure it is leasing tasks.

### 4. Deprovisioning / Hibernation

To suspend billing for an inactive organization, you can destroy the VM while keeping the cache disk, or destroy both.

```bash
terraform destroy -var-file=terraform.tfvars
```
