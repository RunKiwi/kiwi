# Phased Implementation Plan: Startup BYOC Pivot

This document details the engineering phases required to pivot the Kiwi architecture to the BYOC (Bring Your Own Cloud) model designed for fast-moving startups.

## Proposed Phased Execution

### Phase 1: The Data Plane Foundation (`kiwidaemon`)
**Goal:** Build the daemon that will run in the customer's AWS/GCP account to orchestrate sandboxes.
1.  **Daemon Scaffold:** Create the `cmd/kiwidaemon` Go binary.
2.  **Zero-Knowledge Cryptography:** Implement Ed25519 keypair generation on boot and the registration handshake payload.
3.  **Heartbeat Polling:** Implement the HTTPS polling mechanism to pull `worker-spec.json` from the Control Plane.
4.  **LFU Repo Cache:** Implement the `git worktree` isolation logic to create instant, zero-footprint repository clones for tasks.
5.  **Sandbox Spawning:** Integrate the existing `pkg/infra/docker.go` logic to mount the worktree into a secure Docker container upon receiving a spec.

### Phase 2: The Control Plane Adaptations
**Goal:** Update the SaaS backend to support the BYOC model and the Swarm planner.
1.  **Queue Implementation:** Implement the Event Queue to hold `worker-spec.json` payloads waiting for Daemon heartbeats.
2.  **Zero-Knowledge DB Updates:** Modify the `kiwi-api` database schema to store LLM/Git credentials as ciphertext, tied to a Daemon's Public Key.
3.  **Planner API:** Expose the endpoints necessary to trigger the Fable planner, generate the spec, and enqueue it.

### Phase 3: The Integration Layer (CLI & SDK)
**Goal:** Build the developer-native tools for startups to trigger the Swarm.
1.  **`kiwi` CLI Scaffold:** Build the base CLI for `kiwi login` and `kiwi submit`.
2.  **`kiwi claude` Wrapper:** Implement the wrapper that launches local terminal AI tools (like Claude Code) while injecting custom system prompts to teach it how to offload tasks to the Kiwi Swarm.
3.  **Node/Python SDK:** Publish a minimal v1 SDK allowing programmatic task submission (`kiwi.spawn()`) for CI/CD and Sentry integrations.
4.  **Linear Webhook Receiver:** Add an endpoint to the Control Plane to listen for Linear ticket transitions and automatically trigger the Planner.

### Phase 4: Distribution & Onboarding
**Goal:** Make it easy for a startup to deploy the Daemon to their cloud.
1.  **Terraform/CloudFormation Templates:** Author the IaC scripts to provision a secure VPC, VM, install Docker, and start `kiwidaemon`.
