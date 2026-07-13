# Standalone UI Dashboard Architecture (2026 UX Vision)

The current dashboard is an embedded Go string that lacks modern features, robust authentication, and separation of concerns. This plan outlines the transition to a premium, standalone frontend service that mirrors and expands upon the CLI's capabilities.

## 2026 Design Language & Philosophy

Based on design trends from modern, top-tier SaaS startups, the dashboard will adopt a **"Human-Crafted Tactility"** mixed with **"Calm Design."**
*   **Calm Design & Progressive Disclosure:** The interface will hide complex filters and raw JSON logs by default, revealing them only when the user explicitly requests deeper context (e.g., investigating a failed Critic loop).
*   **Tactile & Human-Centric:** We will avoid sterile, flat AI-generated looks. Instead, we will use subtle grain overlays, tactile micro-animations, and high-contrast dark modes to create an interface that feels responsive and grounded.
*   **Liquid Glass & Motion:** Responsive "Liquid Glass" components that blur and react to scrolling, combined with bold typography (e.g., *Inter* or *Outfit*) to create a premium, kinetic identity.
*   **Machine Experience (MX):** Deep semantic HTML structuring to ensure the dashboard is easily parsable by other agentic AI extensions.

## Proposed Tech Stack

*   **Framework:** **Next.js (App Router) + React + TypeScript**. Next.js provides robust routing, SEO capabilities, and built-in API routes that drastically simplify OAuth/OIDC integration for a production SaaS.
*   **Styling:** **Vanilla CSS with CSS Modules**. This guarantees absolute control over complex animations (like Liquid Glass and grain overlays) while keeping styles scoped and maintainable, adhering strictly to our design philosophy.
*   **State Management:** **Zustand**. A modern, lightweight alternative to Redux that excels at managing highly dynamic, real-time states (like streaming agent logs).
*   **Real-time Communication:** Server-Sent Events (SSE) natively consumed via custom React Hooks to stream the agent's live Actor-Critic loop.

## Authentication & Identity

*   **OAuth / SSO:** **GitHub SSO (OIDC)** acting as the primary identity provider, managed via NextAuth.js (or custom Next.js API routes) exchanging tokens with the `kiwi-api`.
*   **Multi-tenant Support:** Org-level isolation mirroring the backend GORM models, allowing for team-based budgeting and task viewing.

## Core Features (Replacing the CLI)

1.  **Task Submission (The "No-CLI" Flow):**
    *   *GitHub Integration:* Users can paste a GitHub repository URL and select a branch. The backend will clone it directly into the sandbox.
    *   *Local Uploads:* Using the HTML5 `<input type="file" webkitdirectory />` API, users can select a local folder in their browser. The frontend will compress it in-browser using a JS library (like JSZip) and upload it to the `/tasks` endpoint, exactly imitating the CLI's behavior.
2.  **Interactive Kanban Board:**
    *   Columns: Backlog, In Progress, Done, Paused/Failed.
    *   Auto-updating cards showing current task cost, agent state, and elapsed time.
3.  **Live Detail View:**
    *   A split-pane view displaying the active agent persona, the streaming SSE logs, and a live rendering of the Git diff as the agent writes code.
4.  **Billing & Settings:**
    *   A dedicated view for the VP of Engineering to track total LLM costs, set budget circuit breakers, and manage team API keys.
