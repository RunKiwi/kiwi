# Dashboard Development Plan: Swarm Control Center

This outlines the development plan for the Kiwi SaaS frontend, pivoting away from a heavy project management tool (Kanban) toward a lightweight, high-performance monitoring interface for the BYOC Swarm.

## 1. 2026 Design Language & Tech Stack
We are keeping our premium technical foundation:
*   **Framework:** Next.js (App Router) + React + TypeScript.
*   **Styling:** Vanilla CSS with "Liquid Glass" overlays and high-contrast dark mode ("Calm Design").
*   **State Management:** Zustand for real-time fleet state.
*   **Data Streaming:** Server-Sent Events (SSE) for zero-latency agent logs.

## 2. Deprecated Features (What we are NOT building)
Based on our BYOC startup pivot, we are scrapping:
*   **[SCRAP] Interactive Kanban Board:** Startups use Linear. We will build a Linear webhook integration instead of recreating a Kanban board.
*   **[SCRAP] HTML5 Local Directory Upload:** Fast-moving teams use Git and CLI, not drag-and-drop browser uploads.

## 3. Core Features (The Swarm Control Center)

### Phase 1: Onboarding & Node Management
*   **The "Add Node" Flow:** A dedicated page where the user selects their cloud provider (AWS/GCP), inputs their API keys (encrypted in-browser via the daemon's public key), and receives a 3-line Terraform snippet to deploy the `kiwidaemon`.
*   **Fleet Status:** A visual representation of active `kiwidaemon` VMs polling for tasks (Green = Polling, Red = Disconnected).

### Phase 2: The "God View" (Parallel Task Monitoring)
Instead of a Kanban board, the main dashboard is a high-density "Swarm View".
*   **Active Swarm:** A real-time grid showing 10-50 tasks running in parallel across the customer's VMs. 
*   **Mini-Status Indicators:** Minimalist progress bars showing if an agent is in the *Planning* (Fable) or *Execution* (Sonnet) phase.

### Phase 3: The Deep-Dive Execution Viewer
When a user clicks on an active task from the God View:
*   **Split-Pane Interface:** 
    *   *Left:* Live SSE streaming logs of the Agent's thought process and bash commands.
    *   *Right:* A real-time unified Git Diff rendering what code the Sandbox is currently modifying.
*   **Manual Eject Button:** A button providing the exact `kiwi pull` CLI command if the user wants to eject the cloud sandbox to their local machine.
