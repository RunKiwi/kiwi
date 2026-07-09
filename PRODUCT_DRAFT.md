# Kiwi: Product Vision & Core Concept

**Codename:** kiwi  
**One-Sentence Pitch:** The Universal Control Plane and Orchestration Layer for Enterprise AI Agents.

---

## 1. Executive Summary

Kiwi bridges the gap between **agentic productivity** and **enterprise compliance**. 

Rather than competing with existing developer tools, Kiwi is designed to seamlessly integrate with and empower the current ecosystem of AI agents (like Aider, Devin, OpenCode, and Claude). It serves as an **Agent-Agnostic Orchestration Layer** that handles secure sandboxing, multi-agent coordination, and credential injection. 

Developers can launch complex workflows that span beyond simple vibe coding—such as data analysis, visualization, and reinforcement learning—safely delegating work to dynamically spawned agent personas while relying on Kiwi's pluggable sandbox architecture and enterprise security guardrails.

---

## 2. The Core Pillars

### I. Universal Agent Orchestration (For the AI Engineer & Developer)
*   **Ecosystem Integration**: Works transparently as the orchestration backend for market-leading tools like Aider, Devin, OpenCode, and Claude. Kiwi increases their enterprise adoption by providing the secure compute and compliance they need.
*   **Multi-Agent Orchestration**: Goes beyond a rigid Actor-Critic algorithm. Kiwi dynamically spawns subagents with specific personas (e.g., Researcher, Data Analyst, Reviewer) based on the task, enabling context sharing and collaboration for complex workflows.
*   **Pluggable Sandboxes**: While providing a robust built-in sandbox, Kiwi also allows customers to bring their own sandbox environments (like E2B or custom Kubernetes clusters) to support heavy data analysis, visual computing, and diverse workloads.

### II. Headless Workflow & Dashboard (For the Developer)
*   **Interactive Kanban Dashboard**: A central web UI showing tasks in a project management board:
    *   *Backlog/Todo*: Tasks queued or scheduled.
    *   *In Progress*: Active agent loops showing live states (e.g., "Editing", "Testing", "Critic Review").
    *   *Done*: Completed tasks with green test suites and merged git branches.
    *   *Paused/Failed*: Tasks halted due to budget limits, stuck loops, or pending manual review.
*   **Continuous Visibility**: The loop runs entirely autonomously in the cloud. If the developer closes their laptop and reopens it later, the CLI simply reconnects to stream the live progress of the ongoing or completed task.

### III. Unified Budget & Execution Safety (For the VP of Engineering)
*   **Single Cost Pool**: Consolidated billing. Admins allocate a single budget (e.g., $5,000 for the team) dynamically distributed across OpenAI, Anthropic, Bedrock, etc.
*   **Loop Protection**: Real-time semantic checking blocks runaway agents from falling into infinite error loops and racking up bills.
*   **Auditable Trajectories**: Complete execution logs—every code change, prompt, compile error, and command execution—are stored in a centralized dashboard.

### IV. Security & Compliance (For the CISO)
*   **Zero-Trust Secrets**: Raw API keys and cloud credentials stay in the enterprise Vault (e.g., HashiCorp, AWS Secrets Manager). Kiwi securely injects them directly into the sandbox environment just-in-time, bypassing the need for brittle reverse tunneling from local laptops.
*   **DLP Guardrails**: Real-time Data Loss Prevention filters out PII, secrets, and private databases before prompts reach public LLM models.

---

## 3. How it Works (The Flow)

1.  **Initiation**: Dev runs `kiwi run --task "Fix bug Y"` on their machine, or assigns a ticket on the **Interactive Kanban Dashboard**.
2.  **Context Sync**: The CLI packages the git status and environment configs and uploads them to an ephemeral Cloud Sandbox.
3.  **Secrets Injection**: Kiwi integrates with the Enterprise Vault to securely mount necessary credentials directly into the sandbox just-in-time.
4.  **Multi-Agent Loop**: 
    *   **Orchestrator** spawns necessary agent personas based on the workload (Data analysis, coding, RL, etc).
    *   **Agents** collaborate, sharing context, while tools like Aider or Claude drive the reasoning.
    *   **Sandbox (Built-in or E2B)** compiles, visualizes, and runs tests.
    *   *Dashboard* updates the task card status in real-time, showing the step-by-step trace.
5.  **Completion**: Once tests pass and the Critic approves, the changes are committed to a git branch, the task card moves to "Done" on the board, and the developer is notified to pull the updates.

---

## 4. Parked Questions & Design Challenges

These are the critical design issues we must resolve to establish Product-Market Fit (PMF):

### Q1: Advanced & Heavy Workloads in the Sandbox
*   *Problem:* How do we support complex use cases like Data Analysis, Visualization, and Reinforcement Learning which require heavy GPU compute or customized runtimes?
*   *Direction:* Shift from a hardcoded Docker approach to a **Pluggable Sandbox Abstraction**. Kiwi will integrate natively with third-party sandbox providers like E2B, Daytona, or custom enterprise Kubernetes clusters, allowing customers to bring their own secure compute environments.

### Q2: Stuck Loops vs. Slow Progress
*   *Problem:* How do we distinguish between an agent stuck in a repetitive loop (e.g., fixing a compile error with the same failed approach) and an agent making slow, incremental progress on a difficult refactoring task?
*   *Resolved:* We implemented a semantic duplicate-error circuit breaker in `pkg/orchestrator/engine.go` that hashes stdout compiler errors and halts execution if the exact same error is encountered 3 times.

### Q3: Credential Injection vs. Reverse Tunnels
*   *Problem:* The current reverse tunnel approach for fetching local secrets is brittle and relies on the developer's laptop remaining online, conflicting with true headless autonomy.
*   *Direction:* Deprecate reverse tunneling in favor of native Enterprise Vault integration. Secrets will be fetched securely server-side and injected directly into the pluggable sandbox via temporary environment variables or mounted volumes, enforcing true zero-trust without local dependencies.

### Q4: Multi-Tenant Isolation
*   *Problem:* How do we securely isolate multiple users and organizations sharing a single Kiwi deployment?
*   *Resolved:* We implemented an organization-scoped multi-tenant architecture. User API keys are validated using SHA-256 hashes via the GORM DB backend. Database tasks are partitioned by `OrgID`, restricting non-admins to their respective workspaces. Reverse credential tunnels inherit task ownership and verify client credentials before multiplexing secret resolution.



