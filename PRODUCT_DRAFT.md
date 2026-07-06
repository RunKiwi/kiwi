# Kiwi: Product Vision & Core Concept

**Codename:** kiwi  
**One-Sentence Pitch:** The Secure Agentic Control Plane for Enterprise Engineering.

---

## 1. Executive Summary

Kiwi bridges the gap between **developer productivity** and **enterprise compliance**. 

It allows software developers to use autonomous coding agents (like Cline, Aider, or custom scripts) under secure, policy-enforced guardrails. Instead of running heavy, expensive, and insecure loops on local developer machines, Kiwi provides an **Agent-Agnostic LLM Proxy** connected to a **Cloud-First Loop Orchestrator**. 

Developers can launch tasks, share context, and safely **close their laptops** while the loop executes, builds, and verifies code inside a secure cloud sandbox.

---

## 2. The Core Pillars

### I. Security & Compliance (For the CISO)
*   **Zero-Trust Secrets**: Raw API keys and cloud credentials stay in the enterprise Vault. The cloud sandbox accesses them ephemerally via OIDC federation or a read-only local reverse tunnel.
*   **DLP Guardrails**: Real-time Data Loss Prevention filters out PII, secrets, and private databases before prompts reach public LLM models.
*   **Auditable Trajectories**: Complete execution logs—every code change, prompt, compile error, and command execution—are stored in a centralized dashboard.

### II. Unified Budget & Loop Safety (For the VP of Engineering)
*   **Single Cost Pool**: Consolidated billing. Admins allocate a single budget (e.g., $5,000 for the team) dynamically distributed across OpenAI, Anthropic, Bedrock, etc.
*   **Loop Protection**: Real-time semantic checking blocks runaway agents from falling into infinite error loops and racking up bills.
*   **Actor-Critic Separation**: A primary "Actor" writes code, while a secondary, cost-efficient "Critic" evaluates tests and security vulnerabilities before changes are accepted.

### III. Headless Workflow & Dashboard (For the Developer)
*   **Interactive Kanban Dashboard**: A central web UI showing tasks in a project management board:
    *   *Backlog/Todo*: Tasks queued or scheduled.
    *   *In Progress*: Active agent loops showing live states (e.g., "Editing", "Testing", "Critic Review").
    *   *Done*: Completed tasks with green test suites and merged git branches.
    *   *Paused/Failed*: Tasks halted due to budget, stuck loops, or waiting for local credential tunnels.
*   **Close-Laptop Autonomy**: The developer kicks off a task via the CLI. Workspace changes sync to a cloud sandbox, which runs the loop headless. The developer can shut down their machine.
*   **Agent Agnostic**: Works transparently with existing IDE extensions, command-line interfaces, and custom Python agent frameworks.
*   **Auto-Resume**: If the laptop is reopened, the local CLI automatically reconnects to the cloud dashboard and streams live progress.

---

## 3. How it Works (The Flow)

1.  **Initiation**: Dev runs `kiwi run --task "Fix bug Y"` on their machine, or assigns a ticket on the **Interactive Kanban Dashboard**.
2.  **Context Sync**: The CLI packages the git status and environment configs and uploads them to an ephemeral Cloud Sandbox.
3.  **Tunneling**: A secure, temporary reverse tunnel is opened to relay local credentials or local validation commands.
4.  **The Loop**: 
    *   **Actor** proposes code edits.
    *   **Sandbox** compiles and runs tests.
    *   **Critic** checks security and convergence.
    *   *Dashboard* updates the task card status in real-time, showing the step-by-step trace.
5.  **Completion**: Once tests pass and the Critic approves, the changes are committed to a git branch, the task card moves to "Done" on the board, and the developer is notified to pull the updates.

---

## 4. Parked Questions & Design Challenges

These are the critical design issues we must resolve to establish Product-Market Fit (PMF):

### Q1: Local Dependencies in the Cloud
*   *Problem:* How do we compile and test code in the cloud sandbox when projects rely on complex local dependencies (e.g., local Postgres databases, proprietary internal services, or macOS-specific toolchains)?
*   *Discussion points:* Do we attempt to auto-containerize the environment, or run execution shims?

### Q2: Stuck Loops vs. Slow Progress
*   *Problem:* How do we distinguish between an agent stuck in a repetitive loop (e.g., fixing a compile error with the same failed approach) and an agent making slow, incremental progress on a difficult refactoring task?
*   *Discussion points:* What thresholds, semantic hashes, or heuristics do we use to prevent false positive halts?

### Q3: Reverse Tunnel Security Concerns
*   *Problem:* Will enterprise network security teams block local reverse tunnels, and how do we provide a clean, non-intrusive alternative for sharing local credentials?

