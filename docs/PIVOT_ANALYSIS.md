# Pivot Analysis: Transitioning to Startup-First

As we pivot the product DNA from a heavy "Enterprise Security" platform to a lightweight, developer-native "Startup Velocity" engine, we need to evaluate our current progress. Here is a breakdown of what we keep, what we scrap, and how we adapt our backlog.

## 1. What We Keep (The Core Engine)

The good news: the hardest technical work we've done is still 100% relevant. The core backend does exactly what we need.
*   **`kiwi-api` (Go Backend):** The orchestration server remains our primary SaaS layer.
*   **Docker Orchestration (`pkg/infra/docker.go`):** The logic to spin up isolated sandboxes is critical. This translates perfectly to the BYOC model (we just provision Docker containers on the user's cloud rather than ours).
*   **Actor-Critic Loop (`pkg/orchestrator/server.go`):** This is our "Loop Engineering" moat. The ability to verify code via automated testing and bounce failures back to the AI is what makes the "Swarm" cost-effective and reliable.

## 2. What Is Wasted / Deprioritized

*   **JIT Secret Broker & RBAC:** We were heavily indexing on complex credential tunneling and role-based access control to sell to enterprise CISOs. In a BYOC startup environment, the startup already owns the cloud. The focus on complex security middleman features can be paused.
*   **Heavy Visual UI Focus (The "Dashboard"):** We recently spent time designing a massive, feature-rich Next.js dashboard mimicking complex SaaS apps. Fast startups prefer CLI, SDKs, and headless integrations (Linear/Slack). The UI should become a lightweight "Control Center" rather than the primary way developers submit tasks.

## 3. Analysis of Current GitHub Issues (Milestone D5)

We recently opened 6 issues for the Standalone UI. Here is what we should do with them:

| Issue | Status | Action / Pivot Rationale |
| :--- | :--- | :--- |
| **#70 Scaffold Next.js App** | **KEEP (Modified)** | We still need a UI, but it should be a lightweight Swarm Control Center to monitor parallel agents, not a heavy IDE. Keep the 2026 aesthetics, but strip the scope. |
| **#71 GitHub OAuth & JWT** | **KEEP** | Standard auth is still required for developers to view their swarm logs on our web portal. |
| **#72 Interactive Kanban Board** | **SCRAP / REPURPOSE** | Startups already use Linear. Building our own Kanban board forces them into a new tool. Instead, this issue should be repurposed into building a **Linear Webhook Integration**. |
| **#73 Task Submission (HTML5 Local Upload)** | **SCRAP** | Startups don't upload directories via browsers. They use git. We should close this and open a new issue for **CLI Task Submission** (`kiwi submit`). |
| **#74 Real-Time Task Detail View (SSE)** | **KEEP** | Highly relevant. When a developer clicks a link from Slack or Linear, they need to see the beautiful, real-time Agent loop execution in the browser. |
| **#75 Billing & Settings Dashboard** | **PAUSE** | Startups using BYOC will have different billing mechanics (charging for orchestration vs. compute). We can delay this until the core SDK/CLI is adopted. |

## 4. What We Need to Create (New Issues)

To execute this pivot, we need to generate new GitHub issues focusing on developer velocity:
1.  **Develop the Kiwi SDK (Node/Python):** The foundation for the CI/CD and Sentry integrations.
2.  **CLI Overhaul (`kiwi claude` & `kiwi pull`):** Build the wrapper for Claude Code local integration and the "Local Eject" feature to pull sandbox states.
3.  **Linear / Slack Integrations:** Build the headless webhook receivers so developers can trigger tasks by dropping a ticket in a specific column or pinging the bot.
4.  **BYOC Terraform Templates:** Create the 1-click deploy scripts (Terraform/CloudFormation) for startups to deploy the sandbox worker nodes into their own AWS/GCP accounts.
