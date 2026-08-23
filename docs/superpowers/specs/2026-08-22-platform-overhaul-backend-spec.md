# Kiwi Platform Overhaul: Backend Design Specification

**Status:** Approved  
**Date:** 2026-08-22  
**Target:** Go Control Plane & Orchestrator API (`pkg/`, `ee/`)  
**Backward Compatibility:** 100% Guaranteed (Non-breaking additive schema & endpoints)

---

## 0. Reconciliation with implemented code (added 2026-08-22, before planning)

This spec was written without checking the current codebase. Several sections
describe things that already exist under different names, or name files that
don't exist. The four implementation plans this spec produced
(`docs/superpowers/plans/2026-08-22-*-backend-plan.md`) already correct for
these; this section is here so nobody re-derives the same wrong assumptions
from the spec directly. Read this before §2 or §3.

- **§2B `UsageRecord` (`ee/billing/models.go`) does not exist** — neither the
  file nor the type, repo-wide. `ee/billing/` holds `billing.go` and
  `stripe.go` only. Cost and token accounting already lives directly on
  `store.Job` (`PlannerCostUSD`, `PlannerTokensIn/Out`) and `store.QueuedTask`
  (`pkg/store/queue_models.go`: `CostUSD`, `Funding`, `TokensIn`, `TokensOut`).
  Do not create `UsageRecord`; add columns to `QueuedTask` instead where the
  spec calls for a new per-usage field.
- **§2D `RunnerNode` (`pkg/store/fleet_models.go`) does not exist and
  shouldn't be added as a parallel table.** `store.Daemon`
  (`pkg/store/daemon_models.go`) is already one row per runner, already
  touched on every heartbeat (`TouchDaemon`). Extend it with nullable metrics
  columns instead.
- **§2E `PRWatchdog` (`pkg/store/monitor_models.go`) is already built** as
  `store.PostMergeMonitor` (`pkg/store/postmerge_monitor_models.go`), served
  by `ee/orchestrator/monitors_api.go` (`POST/GET /api/v1/monitors`,
  `POST /api/v1/monitors/{id}/cancel`) — Phase 1a/1b, already shipped. Do not
  rebuild it.
- **§3 Group E `POST /admin/orgs/{id}/plan` and `POST /admin/orgs/{id}/grant`
  already exist**, at `ee/auth/admin.go:1131` (`handleUpdateOrgPlan`) and
  `:1148` (`handleGrantOrgMinutes`), tested in `ee/auth/admin_test.go`. Their
  response bodies differ from this spec's examples (both return `200` with no
  body, not `{"org_id":..,"plan":..}` / `{"granted_minutes":..}`) — that's the
  existing contract; don't change it to match this doc. Only
  `GET /admin/users` (cross-tenant search) and `GET /admin/metrics/fleet` are
  net new in that group.
- **§3 Group D `GET /api/v1/usage/breakdown` duplicates
  `GET /api/v1/spend`** (`ee/orchestrator/spend_api.go`), which already
  returns `Allowance []AllowanceBucket` (Track 1: tier/period/granted/used/
  remaining, sourced from `ee/entitlement` + `store.OrgTokenGrant`) alongside
  `CostUSD`/`ByProvider`/`KiwiTokensIn/Out` (Track 2). Extend `SpendResponse`
  with the few fields it's missing (limit ceilings, concurrent-lease count);
  do not add a second endpoint.
- **§2A "Multi-Model Routing (Architect vs Worker)" is already fully
  implemented**, end to end: `agent.WorkerSpec.ArchitectModel` (already a
  field), `PlanRequest.ArchitectModel` accepted by
  `POST /api/v1/planner/plan` today, routed through `ee/planner/service.go`
  and consumed by `pkg/daemon/session_run.go:83`. Nothing to build here
  except denormalizing the two model names onto the `Job` row for display.
- **§3 Group A `ee/orchestrator/router.go` and §3 Group B
  `pkg/sandbox/router.go` do not exist.** Routes are registered as
  `mux.HandleFunc` calls in `ee/orchestrator/server.go`. `pkg/sandbox` has no
  HTTP layer at all — it's an execution isolator invoked in-process by the
  daemon (`pkg/daemon`), not a service with routes. Any handler this spec
  places under `pkg/sandbox/cache_api.go` belongs in `ee/orchestrator`
  instead, fed by data the daemon reports over the existing signed heartbeat
  channel — see Plan B.

---

## 1. Problem Statement & Architecture Goals

The Kiwi platform is undergoing an enterprise SaaS upgrade across the dashboard, task pipeline, multi-tenant governance, and telemetry. To support the modernized frontend, the Go backend requires 7 foundational capabilities:

1. **Interactive Plan Mode & Checkpoints**: Let human operators review and approve/reject execution plans before workers modify codebase files.
2. **Sandbox Memory & Workspace Cache**: Track repository git cache hit rates, AST symbol index footprints, and gVisor container memory pressure.
3. **Engineering Velocity & Quality Analytics**: Compute zero-shot vs self-healed test pass rates, 5-stage pipeline turnaround latencies, and AST prompt token caching discounts.
4. **Dual-Track Spend & Quota Metering**: Provide simultaneous metering for Track 1 (Kiwi Token Bucket Quota & minutes remaining) and Track 2 (BYOK Invoiced token spend with spend caps).
5. **Multi-Model Routing (Architect vs Worker)**: Support independent planner model (e.g., `claude-3-7-sonnet`) and worker model (e.g., `claude-3-5-haiku`) routing per job.
6. **Super Admin Governance (Kiwi Staff)**: Cross-tenant user directory search, organization plan tier modifier, compute minute booster injection, and fleet daemon capacity metrics.
7. **Private Fleet & PR Watchdog Telemetry**: Spot lease lifecycle management, runner drain endpoints, and automated branch watchdog tracking.

---

## 2. Database Schema Additions

### A. Additive Columns on `Job` (`pkg/store/models.go`)
```go
type Job struct {
    // Existing fields retained unchanged
    ID               string                 `gorm:"primaryKey" json:"id"`
    OrgID            string                 `gorm:"not null;index" json:"org_id"`
    UserID           string                 `gorm:"not null" json:"user_id"`
    WorkflowID       *string                `json:"workflow_id"`
    ManifestID       *string                `json:"manifest_id"`
    Status           string                 `gorm:"index;not null" json:"status"`
    IdempotencyKey   *string                `gorm:"uniqueIndex:idx_org_idempotency,priority:2" json:"idempotency_key"`
    Inputs           map[string]interface{} `gorm:"type:jsonb;serializer:json;not null" json:"inputs"`
    SandboxRef       *string                `json:"sandbox_ref"`
    CostUSD          float64                `gorm:"not null;default:0" json:"cost_usd"`
    Funding          string                 `gorm:"not null;default:'byok'" json:"funding"`
    PlannerCostUSD   float64                `gorm:"not null;default:0" json:"planner_cost_usd"`
    PlannerTokensIn  int64                  `gorm:"not null;default:0" json:"planner_tokens_in"`
    PlannerTokensOut int64                  `gorm:"not null;default:0" json:"planner_tokens_out"`
    AgentMinutes     float64                `gorm:"not null;default:0" json:"agent_minutes"`
    Error            *string                `json:"error"`
    CreatedAt        time.Time              `gorm:"not null;default:current_timestamp" json:"created_at"`
    UpdatedAt        time.Time              `gorm:"not null;default:current_timestamp" json:"updated_at"`

    // NEW: Additive Plan Mode Fields
    RequiresPlanApproval bool       `gorm:"default:false" json:"requires_plan_approval"`
    PlanStatus           string     `gorm:"type:varchar(32);default:''" json:"plan_status"` // 'drafting', 'pending_review', 'approved', 'rejected'
    PlanMarkdown         string     `gorm:"type:text;default:''" json:"plan_markdown,omitempty"`
    PlanAcceptedAt       *time.Time `json:"plan_accepted_at,omitempty"`
    PlanRejectedReason   string     `gorm:"type:varchar(255);default:''" json:"plan_rejected_reason,omitempty"`

    // NEW: Additive Multi-Model Routing & Spend Cap
    ArchitectModel       string     `gorm:"type:varchar(128);default:'claude-3-7-sonnet'" json:"architect_model"`
    WorkerModel          string     `gorm:"type:varchar(128);default:'claude-3-5-haiku'" json:"worker_model"`
    SpendCapUSD          float64    `gorm:"default:0.50" json:"spend_cap_usd"`
}
```

### B. Additive Columns on `UsageRecord` (`ee/billing/models.go`)
```go
type UsageRecord struct {
    // Existing fields retained
    ID                 string    `gorm:"primaryKey" json:"id"`
    OrgID              string    `gorm:"index;not null" json:"org_id"`
    JobID              string    `gorm:"index;not null" json:"job_id"`
    Provider           string    `gorm:"not null" json:"provider"`
    Model              string    `gorm:"not null" json:"model"`
    TokensIn           int64     `gorm:"not null;default:0" json:"tokens_in"`
    TokensOut          int64     `gorm:"not null;default:0" json:"tokens_out"`
    CreatedAt          time.Time `gorm:"not null;default:current_timestamp" json:"created_at"`

    // NEW: BYOK vs Kiwi-Funded & Prompt Cache Breakdown
    IsBYOK             bool    `gorm:"default:false;index" json:"is_byok"`
    CostUSD            float64 `gorm:"default:0" json:"cost_usd"`
    KiwiCostUSD        float64 `gorm:"default:0" json:"kiwi_cost_usd"`
    CachedPromptTokens int64   `gorm:"default:0" json:"cached_prompt_tokens"`
    RawPromptTokens    int64   `gorm:"default:0" json:"raw_prompt_tokens"`
    DollarSavingsUSD   float64 `gorm:"default:0" json:"dollar_savings_usd"`
}
```

### C. New Model: `WorkspaceCacheEntry` (`pkg/store/cache_models.go`)
```go
type WorkspaceCacheEntry struct {
    ID              string    `gorm:"primaryKey" json:"id"`
    OrgID           string    `gorm:"index;not null" json:"org_id"`
    RepoURL         string    `gorm:"index;not null" json:"repo_url"`
    Branch          string    `gorm:"not null" json:"branch"`
    CommitSHA       string    `gorm:"not null" json:"commit_sha"`
    TreeSizeMB      int64     `gorm:"not null;default:0" json:"tree_size_mb"`
    ASTIndexSizeMB  int64     `gorm:"not null;default:0" json:"ast_index_size_mb"`
    HitCount        int64     `gorm:"not null;default:1" json:"hit_count"`
    LastHitAt       time.Time `gorm:"not null;default:current_timestamp" json:"last_hit_at"`
    CreatedAt       time.Time `gorm:"not null;default:current_timestamp" json:"created_at"`
}
```

### D. New Model: `RunnerNode` (`pkg/store/fleet_models.go`)
```go
type RunnerNode struct {
    ID                 string    `gorm:"primaryKey" json:"id"`
    OrgID              string    `gorm:"index;not null" json:"org_id"`
    HostName           string    `gorm:"not null" json:"host_name"`
    HostIP             string    `gorm:"not null" json:"host_ip"`
    ContainerRuntime   string    `gorm:"default:'runsc'" json:"container_runtime"` // 'runsc' (gVisor) or 'runc'
    Status             string    `gorm:"default:'idle'" json:"status"`             // 'idle', 'leased', 'draining', 'offline'
    AllocatedMemoryMB  int64     `gorm:"default:4096" json:"allocated_memory_mb"`
    UsedMemoryMB       int64     `gorm:"default:0" json:"used_memory_mb"`
    ActiveContainers   int       `gorm:"default:0" json:"active_containers"`
    MaxCapacity        int       `gorm:"default:8" json:"max_capacity"`
    ColdStartLatencyMS float64   `gorm:"default:0" json:"cold_start_latency_ms"`
    LastHeartbeatAt    time.Time `gorm:"not null;default:current_timestamp" json:"last_heartbeat_at"`
}
```

### E. New Model: `PRWatchdog` (`pkg/store/monitor_models.go`)
```go
type PRWatchdog struct {
    ID             string    `gorm:"primaryKey" json:"id"`
    OrgID          string    `gorm:"index;not null" json:"org_id"`
    Repo           string    `gorm:"not null" json:"repo"`
    TargetBranch   string    `gorm:"default:'main'" json:"target_branch"`
    AutoRemediate  bool      `gorm:"default:true" json:"auto_remediate"`
    TriggerOn      string    `gorm:"default:'pr_opened,pr_comment'" json:"trigger_on"`
    PassRatePct    float64   `gorm:"default:100.0" json:"pass_rate_pct"`
    TotalRuns      int64     `gorm:"default:0" json:"total_runs"`
    Status         string    `gorm:"default:'active'" json:"status"`
    CreatedAt      time.Time `gorm:"not null;default:current_timestamp" json:"created_at"`
}
```

---

## 3. Complete REST / JSON API Specifications

### Group A: Plan Mode Endpoints
* **`GET /api/v1/jobs/{id}/plan`**
  * Headers: `Authorization: Bearer <jwt>`
  * Response (200 OK):
    ```json
    {
      "job_id": "job_123",
      "plan_status": "pending_review",
      "plan_markdown": "# Plan\n1. Modify auth.go\n2. Add test",
      "requires_approval": true,
      "architect_model": "claude-3-7-sonnet",
      "created_at": "2026-08-22T19:00:00Z"
    }
    ```
* **`POST /api/v1/jobs/{id}/plan/approve`**
  * Headers: `Authorization: Bearer <jwt>`
  * Request Body: `{ "user_comment": "Approved by reviewer" }`
  * Response (200 OK): `{ "status": "approved", "resumed_phase": "actor" }`
* **`POST /api/v1/jobs/{id}/plan/reject`**
  * Headers: `Authorization: Bearer <jwt>`
  * Request Body: `{ "feedback": "Please use CockroachDB leases instead" }`
  * Response (200 OK): `{ "status": "rejected", "planner_notified": true }`

### Group B: Sandbox Cache & Memory Endpoints
* **`GET /api/v1/sandbox/cache/stats`**
  * Response (200 OK):
    ```json
    {
      "cache_hit_rate_pct": 94.2,
      "total_cached_trees": 18,
      "storage_footprint_mb": 1420,
      "ast_index_footprint_mb": 340,
      "bandwidth_saved_mb": 84200,
      "avg_clone_latency_ms": 320.5
    }
    ```
* **`POST /api/v1/sandbox/cache/evict`**
  * Request Body: `{ "repo_url": "github.com/acme-corp/core-api", "commit_sha": "abc123" }`
  * Response (200 OK): `{ "evicted_entries": 1, "freed_mb": 82 }`
* **`GET /api/v1/sandbox/memory/metrics`**
  * Response (200 OK):
    ```json
    {
      "total_allocated_mb": 32768,
      "total_used_mb": 12400,
      "container_cgroup_pressures": [
        { "container_id": "c-01", "rss_mb": 1024, "limit_mb": 4096, "oom_guard_headroom_pct": 75.0 }
      ]
    }
    ```

### Group C: Engineering Velocity & Quality Telemetry Endpoints
* **`GET /api/v1/analytics/velocity?range=7d`**
  * Response (200 OK):
    ```json
    {
      "test_pass_metrics": {
        "zero_shot_pct": 72.4,
        "self_healed_pct": 21.1,
        "human_guided_pct": 6.5
      },
      "pipeline_stage_latencies": {
        "clone_and_provision_sec": 4.2,
        "env_prep_sec": 12.5,
        "ast_edit_sec": 45.1,
        "test_guard_sec": 28.3,
        "review_sec": 15.0
      },
      "plan_acceptance_metrics": {
        "first_pass_accepted_pct": 84.5,
        "avg_review_turnaround_sec": 142.0
      }
    }
    ```
* **`GET /api/v1/analytics/caching`**
  * Response (200 OK):
    ```json
    {
      "cached_prompt_tokens": 142800000,
      "raw_prompt_tokens": 15600000,
      "cache_discount_rate": 0.90,
      "total_dollar_savings_usd": 1284.50
    }
    ```
* **`GET /api/v1/analytics/repos`**
  * Response (200 OK):
    ```json
    {
      "repos": [
        { "repo": "acme-corp/core-api", "total_spend_usd": 8.40, "tokens": 42000000, "prs_created": 14, "pr_merge_yield_pct": 92.8 },
        { "repo": "acme-corp/analytics-engine", "total_spend_usd": 3.20, "tokens": 18000000, "prs_created": 6, "pr_merge_yield_pct": 100.0 }
      ]
    }
    ```

### Group D: Dual-Track Spend & Quota Endpoints
* **`GET /api/v1/usage/breakdown`**
  * Response (200 OK):
    ```json
    {
      "quota_track": {
        "tier": "free",
        "agent_minutes_used": 412.0,
        "agent_minutes_limit": 500.0,
        "tokens_used": 2840000,
        "tokens_limit": 5000000,
        "concurrent_leases_active": 1,
        "concurrent_leases_max": 2,
        "resets_at": "2026-09-01T00:00:00Z"
      },
      "byok_track": {
        "is_byok_enabled": true,
        "invoiced_cost_usd": 14.30,
        "spend_cap_usd": 50.00,
        "provider_spend": {
          "anthropic": 8.40,
          "openai": 4.20,
          "deepseek": 1.70
        }
      }
    }
    ```
* **`PUT /api/v1/jobs/{id}/spend-cap`**
  * Request Body: `{ "spend_cap_usd": 1.50 }`
  * Response (200 OK): `{ "job_id": "job_123", "spend_cap_usd": 1.50 }`

### Group E: Super Admin Endpoints (Kiwi Staff Only)
* **`GET /admin/stats`** *(Auth: KIWI_SUPER_ADMIN_EMAILS or KIWI_SERVER_TOKEN)*
  * Returns global organization count, 7d/30d signups, compute volume, and provider invoice matrix.
* **`GET /admin/users?search=&limit=50&offset=0`**
  * Response (200 OK):
    ```json
    {
      "users": [
        {
          "id": "usr_01",
          "email": "sarah@stripe.com",
          "name": "Sarah Chen",
          "org_id": "org-stripe-1102",
          "org_name": "Stripe Core Eng",
          "role": "admin",
          "auth_provider": "google",
          "created_at": "2026-05-18T10:00:00Z",
          "last_active_at": "2026-08-22T19:40:00Z"
        }
      ],
      "total": 412
    }
    ```
* **`POST /admin/orgs/{id}/plan`**
  * Request Body: `{ "plan": "enterprise" }`
  * Response (200 OK): `{ "org_id": "org_123", "plan": "enterprise" }`
* **`POST /admin/orgs/{id}/grant`**
  * Request Body: `{ "minutes": 1000 }`
  * Response (200 OK): `{ "org_id": "org_123", "granted_minutes": 1000, "new_balance": 1088 }`
* **`GET /admin/metrics/fleet`**
  * Response (200 OK):
    ```json
    {
      "host_pool": "gce-us-central1",
      "active_containers": 14,
      "max_capacity": 32,
      "queue_depth": 6,
      "avg_cold_start_ms": 4200.0,
      "imds_blocked_count": 1842
    }
    ```

---

## 4. Security & Multi-Tenant Isolation Rules

1. Every non-superadmin route MUST be gated through `authorizeOrgAccess(r, orgID)`.
2. Every database query in `ee/orchestrator` and `pkg/store` MUST filter by `org_id = ?`.
3. Secret zero-leak: API keys and auth credentials are never included in API responses or plain text logs.
