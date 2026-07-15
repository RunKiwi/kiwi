# Kiwi: The AI Execution Engine for Fast-Moving Startups

## The Problem
Fast-moving startups have an infinite product backlog but limited runway. Hiring engineers is slow and expensive, and the engineers you *do* have are bogged down writing boilerplate, fixing broken tests, and managing tech debt rather than shipping core IP. Current AI "copilots" require constant hand-holding, and existing cloud agents are rigid, opaque, and expensive.

## The Solution
Kiwi is an autonomous cloud execution engine that works while you sleep. We provide a lightweight, API-first orchestration layer that spins up fleets of AI agents to tackle your backlog in parallel. It’s like hiring a team of 10x engineers that never log off. 

---

## The 5 Core Pillars

### 1. BYOC (Bring Your Own Cloud) First
Startups want AI leverage without sacrificing data privacy or failing compliance audits. Kiwi runs its dedicated agent sandboxes directly inside *your* AWS/GCP account. Your code and data never leave your infrastructure. For us, this eliminates massive compute overhead, keeping our margins high and prices low.

### 2. The SDK: Infrastructure, Not Just a Tool
Kiwi isn't just a UI you log into; it's infrastructure you build upon. With our Node/Python SDK, teams can wire Kiwi directly into their event streams:
*   **Self-Healing CI/CD:** A failing GitHub Action automatically triggers `kiwi.spawn()` to spin up an agent, fix the broken test, and push a patch.
*   **Sentry Auto-Triage:** When a production exception fires, a webhook triggers a Kiwi agent to investigate the stack trace in a secure sandbox and open a draft PR before the on-call engineer wakes up.

### 3. The Swarm (Massive Parallelization at Micro-Costs)
Because Kiwi leverages smart **Loop Engineering** (our proprietary Actor-Critic loop), we don't rely solely on expensive frontier models like GPT-4. We use expensive models for high-level architectural planning, and route the repetitive grunt work (testing, compiling, refactoring) to cheaper, faster models. This allows startups to spin up 50 parallel agents to clear a backlog overnight for the cost of a cup of coffee.

### 4. Headless & Invisible Workflows
Startups don't have time to learn a new opinionated platform. Kiwi integrates where they already work. 
*   **Linear Integration:** Drag a ticket to the "Kiwi" column, and the swarm goes to work. 
*   **Local CLI & `kiwi claude`:** Our CLI seamlessly integrates with local terminal AI tools (like Claude Code). By wrapping the local chat, Claude acts as the planner in your terminal, and automatically uses the `kiwi` CLI as a tool to offload massive refactors to the cloud swarm when it gets overwhelmed.

### 5. Local Eject (The Steering Wheel)
Autopilot when you want it, manual override when you need it. If a cloud agent gets stuck on the final 10% of a complex feature, developers can run `kiwi pull` to instantly download the cloud sandbox state to their local machine and finish the job manually. No more getting stuck waiting on an AI black box.
