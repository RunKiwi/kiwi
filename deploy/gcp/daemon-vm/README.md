# Kiwi Managed Tier: Daemon VM

This Terraform module provisions a dedicated, per-org execution daemon (the Data Plane) in the Kiwi Managed GCP environment.

## Architecture

*   **Hostile-by-Default:** The daemon VM has no ambient GCP credentials. It runs under the default compute service account with an empty scope list, meaning it cannot mint access tokens for GCP APIs. Project-wide SSH keys are blocked, and OS Login is disabled.
*   **Shielded VM:** The VM is configured with Secure Boot, vTPM, and Integrity Monitoring enabled to prevent rootkits and ensure boot integrity. Serial console access is disabled.
*   **Network Isolation & Egress:** The instance resides in a dedicated `daemon` subnet with no public IPs. A default-deny egress firewall rule drops all outbound traffic. Outbound access is permitted *only* for DNS and specific allowlisted CIDRs (VCS, Model APIs, and the Kiwi Public API) via Cloud NAT. There is no inbound route.
*   **Nested Virtualization:** Enabled at the hardware level, allowing Firecracker microVMs to run securely within this instance.
*   **Persistent Cache:** A dedicated Google Persistent Disk is attached and mounted at `/mnt/kiwi-cache` for the daemon to persist its `gitcache` across restarts.
*   **Metadata Injection:** The join token and org ID are provided to the instance via instance metadata, avoiding baked-in secrets.
*   **Startup Script:** Automatically formats the cache disk on first boot, installs Docker, and runs the `kiwidaemon` container connected to the Control Plane API.

## Extending the Egress Allowlist

To allow egress to a new model provider or VCS, you must update the `allowed_egress_cidrs` variable in the `control-plane` Terraform module:

1. Identify the CIDR blocks for the new provider.
2. Add them to `allowed_egress_cidrs` in your `terraform.tfvars` for the `control-plane`.
3. Apply the `control-plane` module to update the `kiwi-daemon-egress-allow-https` firewall rule.

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
