# RFC: Startup-First BYOC Platform Pivot

**Date:** 2026-07-16
**Status:** Proposed

## 1. Summary
This RFC proposes a strategic pivot of the Kiwi platform away from a heavy, enterprise-security-focused SaaS (with a complex Kanban UI) toward a lightweight, developer-native, BYOC (Bring Your Own Cloud) Execution Engine tailored for fast-moving startups. The new architecture emphasizes massive parallelization (The Swarm), zero-knowledge credential sharing, `git worktree` caching, and programmatic integrations (SDK/CLI).

## 2. Motivation
Startups face an infinite backlog but have limited runway and engineering headcount. Current AI copilots require too much human hand-holding, while existing cloud agents (like Vorflux) are rigid, opaque, and expensive. 

By pivoting to a BYOC model, we achieve two major goals:
1. **Cost & Privacy for Users:** Startups run the executing agents in their own AWS/GCP accounts, ensuring proprietary code never leaves their VPC and avoiding strict compliance audits.
2. **Zero Compute Overhead for Kiwi:** We act purely as the orchestration and planning layer, maintaining high margins.

## 3. Architecture Proposal (Control Plane / Data Plane)

The architecture splits into a central SaaS (Control Plane) and a localized agent runner (Data Plane).

### 3.1 High-Level Structure

```mermaid
graph TD
    subgraph SaaS[Kiwi SaaS - Control Plane]
        UI[SDK / CLI / Webhooks]
        API[API Gateway & Auth]
        DB[(SaaS Database<br>Encrypted Secrets)]
        Q[(Event Queue)]
        Planner[Orchestrator<br>Planner Agent]
    end

    subgraph BYOC[Customer VPC - Data Plane]
        KD[KiwiDaemon<br>VM / Kubernetes]
        Cache[(LFU Git Worktree Cache)]
        SB1[Docker Sandbox 1<br>Execution Agent]
        SB2[Docker Sandbox N<br>Execution Agent]
    end

    subgraph External[External Services]
        LLM_P[Frontier LLM<br>e.g., Fable]
        LLM_W[Worker LLM<br>e.g., Sonnet]
        VCS[GitHub / GitLab]
    end

    %% SaaS Internal
    UI --> API
    API --> Planner
    API --> DB
    Planner --> Q
    
    %% SaaS to External
    Planner <-->|Plan generation| LLM_P
    
    %% Data Plane Internal
    KD --> Cache
    KD --> SB1
    KD --> SB2
    
    %% Data Plane to SaaS
    KD <-->|Network Pull Model<br>HTTPS Polling| API
    
    %% Data Plane to External
    SB1 <-->|Execute code| LLM_W
    SB2 <-->|Execute code| LLM_W
    Cache <-->|git fetch| VCS
    SB1 -->|Push PR| VCS
```

### 3.2 Execution Flow

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
    U->>API: Add Provider Key (Stored in SaaS DB)
    U->>KD: Run `terraform apply` in AWS/GCP
    KD->>API: Boot & Self-Register (Sends KD Public Key)
    
    Note over U, Q: 2. Task Planning (The Swarm)
    U->>API: `kiwi submit "Fix issue #50"`
    API->>CP: Route Task
    CP->>LLM: Planner Agent (Fable) generates `worker-spec.json`
    LLM-->>CP: Returns chunked DAG of tasks, AGENT.md, deps
    CP->>CP: Encrypt API Keys using KD Public Key
    CP->>Q: Enqueue `worker-spec.json` (with encrypted keys)
    
    Note over KD, SB: 3. Execution (Pull Model)
    KD->>API: HTTPS Heartbeat (Network Pull)
    API->>Q: Pop task
    API-->>KD: Send `worker-spec.json`
    KD->>KD: Decrypt Keys in memory using KD Private Key
    KD->>KD: `git fetch` & `git worktree add /tmp/task-123`
    KD->>SB: Mount worktree into Docker Sandbox
    SB->>LLM: Execution Loop (Sonnet) writes code & tests
    SB-->>KD: Tests Pass -> PR Created
    KD->>KD: Unmount & `git worktree remove`
    KD->>API: Report Success
```

## 4. Technical Deep Dives

### 4.1 Secure Credential Management (Asymmetric Encryption)
To securely transmit credentials (LLM keys, Git tokens) to the customer's VPC without exposing them in transit:
* `KiwiDaemon` boots on a VM and generates an Ed25519 keypair, registering its Public Key with the CP.
* The SaaS Control Plane holds **its own** Global API keys securely to communicate with the Frontier Model for planning.
* **Customer-provided credentials** (e.g., Worker LLM keys, Git tokens) are stored in the SaaS database only in zero-knowledge encrypted form using the `KiwiDaemon`'s Public Key. The SaaS never has plaintext access to them.
* KD pulls the payload via HTTPS polling (Pull Model) and decrypts the customer credentials in-memory during execution.

### 4.2 LFU Repository Caching (`git worktree`)
To avoid expensive network clones for parallel agents:
* KD maintains a base `bare` clone of the repository.
* When a task arrives, KD runs `git worktree add /tmp/task-123 main` (milliseconds latency, zero extra disk space).
* The Docker Sandbox mounts `-v /tmp/task-123:/workspace`.
* Post-execution, the worktree is destroyed via `git worktree remove`.

## 5. Phased Implementation Plan

### Phase 1: Data Plane Foundation (`kiwidaemon`)
* Scaffold `cmd/kiwidaemon` in Go.
* Implement Ed25519 cryptography and registration handshake.
* Implement HTTPS heartbeat polling for `worker-spec.json`.
* Implement `git worktree` isolation and Docker sandbox mounting.

### Phase 2: Control Plane Adaptations
* Implement the Event Queue for pending tasks.
* Update `kiwi-api` DB schema for secure credential storage.
* Expose endpoints for Fable planner triggers.

### Phase 3: Integration Layer (CLI & SDK)
* Build `kiwi login` and `kiwi submit` CLI.
* Implement `kiwi claude` local wrapper to route terminal commands to the Swarm.
* Publish minimal Node/Python SDK for CI/CD integrations.
* Build Linear Webhook receivers.

### Phase 4: Distribution
* Author Terraform/CloudFormation templates for 1-click customer deployment.

## 6. Drawbacks & Alternatives
* **Drawback:** BYOC requires the customer to manage a VM, which adds initial friction compared to a pure SaaS offering.
* **Mitigation:** Provide robust, copy-paste Terraform modules that handle VPC, VM, and networking automatically.
* **Alternative:** Fully hosted SaaS for execution. (Ruled out currently due to massive compute overhead and runway costs, but will be offered later as a premium tier).
