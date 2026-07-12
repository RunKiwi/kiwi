# Deployment & Kiwi Runner Daemon — Implementation Plan

**Goal:** Provide a frictionless local development experience via Docker Compose and an enterprise-ready SaaS Bring-Your-Own-Cloud (BYOC) deployment model using the outbound-polling Kiwi Runner Daemon.

This document breaks down the deployment architecture added to the RFC (`docs/rfcs/2026-07-10-agentic-execution-platform-rfc.md`) into concrete engineering workstreams.

## 1. Local Development (Docker Compose)
**Objective:** Allow developers to spin up the entire distributed control plane on their laptops with a single command.

### Workstream D1: The `docker-compose.yml` Stack
- **Tasks:**
  - [ ] Write a `docker-compose.yml` defining the following services:
    - `postgres`: (PostgreSQL 16) for the CP source of truth.
    - `nats`: (NATS JetStream) for the durable job queue and event bus.
    - `minio`: (S3-compatible) for checkpoint and artifact object storage.
    - `kiwi-api`: The API server role (runs `kiwid -role api`).
    - `kiwi-llmo`: The LLM Orchestrator role (runs `kiwid -role orchestrator`).
  - [ ] Create an init script (`scripts/init-db.sh`) to run Postgres migrations on boot.
  - [ ] Configure Docker networking so `kiwi-llmo` can communicate with `nats` and `postgres`.
  - [ ] Add a `Makefile` target `make run-local` wrapping the compose up command.
- **Exit Criteria:** A developer can clone the repo, run `make run-local`, and successfully submit a job to the API that propagates to the queue.

## 2. Production SaaS & BYOC Runner Daemon
**Objective:** Build the `kiwi-runner` binary that runs in the customer's VPC, polls the SaaS event bus, and executes sandboxes locally using native OIDC cloud secrets.

### Workstream D2: The `kiwi-runner` Daemon
- **Modules:** `cmd/kiwi-runner/` (new entrypoint), `pkg/runner/` (polling logic).
- **Tasks:**
  - [ ] Create the `kiwi-runner` binary. It requires arguments: `--saas-url`, `--runner-token`, `--labels`.
  - [ ] Implement an outbound gRPC/WebSocket client that connects to the SaaS Control Plane to claim jobs for its tenant/labels.
  - [ ] Implement the polling loop: fetch job manifest -> execute via local `Infra` driver (Docker/K8s) -> stream events back to SaaS -> upload checkpoint snapshots directly to configured S3 bucket.
- **Exit Criteria:** The runner can successfully claim and execute a job from a remote SaaS control plane without any inbound firewall rules.

### Workstream D3: SaaS Runner Orchestration (Control Plane side)
- **Modules:** `pkg/orchestrator/` (routing jobs to runners).
- **Tasks:**
  - [ ] Define a `RunnerInfra` driver satisfying the `Infra` interface. Instead of spinning up a local container, this driver places the job on a NATS subject specific to the customer's tenant and waits for a runner to claim it.
  - [ ] Add Runner authentication/authorization to the API server (validating the `--runner-token`).
  - [ ] Implement heartbeat monitoring: if a runner stops sending heartbeats, the SaaS orchestrator requeues the job.
- **Exit Criteria:** A job submitted to the SaaS with a "BYOC" flag is routed to the customer's runner queue and successfully executed by the remote runner.

### Workstream D4: Native Cloud Secrets (OIDC)
- **Modules:** `pkg/secrets/`
- **Tasks:**
  - [ ] Add an AWS Secrets Manager provider and a GCP Secret Manager provider to the JIT secret broker.
  - [ ] Update the sandbox Agent API so that when a secret is requested, the runner daemon intercepts it and uses its host's IAM role (via AWS STS or GCP default credentials) to fetch the secret locally.
  - [ ] Ensure secrets are never sent back to the SaaS control plane.
- **Exit Criteria:** A sandbox running on an AWS EC2 instance can successfully fetch a database password from AWS Secrets Manager using the EC2 instance profile, without any credentials hardcoded in the manifest.
