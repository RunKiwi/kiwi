# Kiwi Architecture Design (Startup-First BYOC)

This document outlines the architectural blueprint for the Kiwi AI Execution Engine, emphasizing a startup-friendly, BYOC (Bring Your Own Cloud) model, highly parallelized Swarm capabilities, and zero-knowledge security.

## Core Tenets
1.  **Decoupled Control & Data Planes:** Kiwi SaaS handles orchestration (planning); customer clouds handle execution (building).
2.  **Zero-Knowledge Credentials:** API Keys and Git tokens are encrypted at the edge and never stored in plaintext by the SaaS.
3.  **Git Worktree Caching:** Utilize `git worktree` for instant repository cloning inside the Data Plane (bounded LFU eviction is a planned follow-up).
4.  **CLI & SDK Native:** Emphasize programmatic integration over heavy UI dashboards.

---

## 1. High-Level Architecture Flow

The architecture is split into two halves: the SaaS Control Plane and the Customer's Data Plane (BYOC).

```mermaid
sequenceDiagram
    participant U as User (SDK/CLI/UI)
    box rgba(61, 91, 255, 0.1) Control Plane (Kiwi SaaS)
    participant API as API Gateway & Auth
    participant CP as Orchestrator (Go)
    participant Q as Event Queue
    end
    box rgba(200, 200, 200, 0.1) Data Plane (Customer BYOC)
    participant KD as KiwiDaemon (VM)
    participant SB as Sandbox (Docker)
    end
    participant LLM as LLM Provider (Fable/Sonnet)

    Note over U, API: 1. Setup & Registration
    U->>API: Add Provider Key (Encrypted in Browser via KD PubKey)
    U->>KD: Run `terraform apply` in AWS/GCP
    KD->>API: Boot & Self-Register (Sends PubKey)
    
    Note over U, Q: 2. Task Planning (The Swarm)
    U->>API: `kiwi submit "Fix issue #50"` (via CLI or Linear Webhook)
    API->>CP: Route Task
    CP->>LLM: Planner Agent (Fable) generates `worker-spec.json`
    LLM-->>CP: Returns chunked DAG of tasks, AGENT.md, deps
    CP->>Q: Enqueue `worker-spec.json` 
    
    Note over KD, SB: 3. Execution (Zero-Knowledge)
    KD->>API: HTTPS Heartbeat (Polling)
    API->>Q: Pop task
    API-->>KD: Send `worker-spec.json` (Includes Encrypted Keys)
    KD->>KD: Decrypt Keys in memory using Private Key
    KD->>KD: `git fetch` & `git worktree add /tmp/task-123`
    KD->>SB: Mount worktree into Docker Sandbox
    SB->>LLM: Execution Loop (Sonnet) writes code & tests
    SB-->>KD: Tests Pass -> PR Created
    KD->>KD: Unmount & `git worktree remove`
    KD->>API: Report Success
```

---

## 2. The Components

### Control Plane (SaaS)
*   **API Gateway & Auth:** Handles user authentication, team RBAC, and exposes the SDK/CLI/Webhook endpoints.
*   **Orchestrator (`kiwi-api` in Go):** Houses the Planner logic. It uses frontier models (like Fable) to break a high-level task down into a DAG (Directed Acyclic Graph) of smaller jobs (`worker-spec.json`).
*   **Event Queue:** A highly scalable message broker (e.g., Redis Streams or SQS) holding pending worker specs for the Daemons to pick up.

### Data Plane (BYOC)
*   **KiwiDaemon (KD):** A lightweight Go binary running on a dedicated VM (EC2/Compute Engine) inside the customer's VPC. It is responsible for polling the queue, managing the repo cache, decrypting secrets in memory, and spawning sandboxes.
*   **The Sandbox:** Ephemeral, isolated environments (Docker containers or Firecracker microVMs) where the actual Worker agents (using cheaper models like Sonnet) execute code, run shell commands, and verify test suites.

### Integration Layer
*   **The SDK:** A Node/Python library allowing programmatic task submission (e.g., `kiwi.spawn()`).
*   **CLI (`kiwi`):** Local developer tool for submitting tasks, streaming logs, and running `kiwi claude` (wrapping local AI terminals to offload large refactors to the Swarm).
*   **Headless Connectors:** Native webhooks for Linear, GitHub, and Slack to trigger the Swarm without opening a dashboard.

---

## 3. Deep Dives

### Zero-Knowledge Credential Sharing
The SaaS database only stores ciphertext.
1. `KiwiDaemon` boots and generates an Ed25519 keypair. The Private Key stays on the VM disk. The Public Key is sent to the Control Plane.
2. When the user configures API keys or Git tokens in the Kiwi UI, the browser encrypts them using the KD's Public Key before transmission.
3. During the daemon heartbeat, the Control Plane sends the ciphertext. The daemon decrypts it in-memory.

### Lightning-Fast Sandbox Provisioning
We eliminate the network latency of cloning large mono-repos.
1. `KiwiDaemon` maintains a base `bare` clone on the VM.
2. For each task, it runs `git worktree add /tmp/task-123 main` (which takes milliseconds).
3. The Sandbox container mounts `-v /tmp/task-123:/workspace`.
4. When finished, the worktree is destroyed.
