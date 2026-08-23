# Backend Implementation Plan: Kiwi Platform Overhaul

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement the Go backend data models, GORM schema additions, and REST/JSON API endpoints to support Plan Mode, Sandbox Memory & Workspace Cache telemetry, Engineering Velocity analytics, Dual-Track Spend & Quotas, Private Fleet node telemetry, and the Super Admin Governance console.

**Architecture:** Additive, non-breaking schema migrations in `pkg/store/` and `ee/billing/`. Handler logic in `ee/orchestrator/`, `pkg/sandbox/`, `ee/provisioner/`, and `ee/auth/`. Access control strictly gated through `authorizeOrgAccess` (tenant-scoped) and `isAdminAuthorized` (super-admin scoped).

**Tech Stack:** Go 1.25, GORM / PostgreSQL, Docker + gVisor runtime.

**Spec:** `docs/superpowers/specs/2026-08-22-platform-overhaul-backend-spec.md`

## Global Constraints

- `gofmt -l cmd/ pkg/ ee/` must print nothing; `go test ./pkg/...` and `go test ./ee/...` must pass before every commit.
- Everything under `ee/` carries the `LicenseRef-Kiwi-BSL-1.1` header; `ee/` may import Apache-2.0 packages (`pkg/`), never the reverse.
- Every new table and row is `org_id`-scoped; every orchestrator query filters on it.
- Backward compatibility: All new columns on `jobs` and `usage_records` must be nullable or have safe defaults (`DEFAULT ''` or `DEFAULT 0`).

---

## Phase 1: Data Model & Database Migrations

### Task 1: Add Plan Mode & Multi-Model Routing fields to Job Model
**Files:**
- Modify: `pkg/store/models.go`
- Create: `migrations/0044_plan_mode_and_routing.up.sql`
- Test: `pkg/store/job_test.go`

- [ ] **Step 1: Write failing store tests for Job Plan Mode fields**
```go
func TestJobPlanModeFields(t *testing.T) {
    s := newTestStore(t)
    job := &Job{
        ID: "job-plan-test",
        OrgID: "org-1",
        UserID: "usr-1",
        Status: "PENDING",
        RequiresPlanApproval: true,
        PlanStatus: "pending_review",
        PlanMarkdown: "# Plan\n1. Step 1",
        ArchitectModel: "claude-3-7-sonnet",
        WorkerModel: "claude-3-5-haiku",
        SpendCapUSD: 0.75,
    }
    err := s.CreateJob(context.Background(), job)
    require.NoError(t, err)
    
    fetched, err := s.GetJob(context.Background(), "job-plan-test")
    require.NoError(t, err)
    require.True(t, fetched.RequiresPlanApproval)
    require.Equal(t, "pending_review", fetched.PlanStatus)
    require.Equal(t, 0.75, fetched.SpendCapUSD)
}
```

- [ ] **Step 2: Update `pkg/store/models.go` and write SQL migration**
- [ ] **Step 3: Run `go test ./pkg/store/...` and verify pass**

---

### Task 2: Add Workspace Cache & Fleet Models
**Files:**
- Create: `pkg/store/cache_models.go`
- Create: `pkg/store/fleet_models.go`
- Create: `pkg/store/monitor_models.go`
- Create: `migrations/0045_cache_and_fleet_models.up.sql`
- Test: `pkg/store/cache_test.go`, `pkg/store/fleet_test.go`

- [ ] **Step 1: Write failing store tests for WorkspaceCacheEntry, RunnerNode, and PRWatchdog**
- [ ] **Step 2: Implement struct models and GORM AutoMigrate / SQL up scripts**
- [ ] **Step 3: Run `go test ./pkg/store/...` and verify pass**

---

## Phase 2: Plan Mode Orchestration Endpoints

### Task 3: Implement Plan Retrieval, Approve, and Reject Handlers
**Files:**
- Create: `ee/orchestrator/plan_api.go`
- Modify: `ee/orchestrator/router.go`
- Test: `ee/orchestrator/plan_api_test.go`

- [ ] **Step 1: Write HTTP handler tests for `GET /api/v1/jobs/{id}/plan`, `POST /approve`, `POST /reject`**
- [ ] **Step 2: Implement `handleGetJobPlan`, `handleApproveJobPlan`, `handleRejectJobPlan`**
- [ ] **Step 3: Connect approval event to resume worker implementer phase (`actor`)**
- [ ] **Step 4: Run `go test ./ee/orchestrator/...` and verify pass**

---

## Phase 3: Sandbox Memory & Workspace Cache API

### Task 4: Implement Sandbox Cache Stats & Memory Pressure Endpoints
**Files:**
- Create: `pkg/sandbox/cache_api.go`
- Modify: `pkg/sandbox/router.go`
- Test: `pkg/sandbox/cache_api_test.go`

- [ ] **Step 1: Write tests for `GET /api/v1/sandbox/cache/stats` and `POST /evict`**
- [ ] **Step 2: Implement stats aggregator from `pkg/gitcache` and disk layer**
- [ ] **Step 3: Run `go test ./pkg/sandbox/...` and verify pass**

---

## Phase 4: Velocity Analytics & Dual Spend Metering

### Task 5: Implement Velocity, Caching, and Dual Spend Breakdown APIs
**Files:**
- Create: `ee/orchestrator/analytics_api.go`
- Modify: `ee/orchestrator/usage_api.go`
- Test: `ee/orchestrator/analytics_api_test.go`

- [ ] **Step 1: Write tests for `GET /api/v1/analytics/velocity`, `GET /api/v1/analytics/caching`, and `GET /api/v1/usage/breakdown`**
- [ ] **Step 2: Implement test pass aggregation (zero-shot, self-healed, human-guided)**
- [ ] **Step 3: Implement prompt caching token discount calculations (90% off cached tokens)**
- [ ] **Step 4: Implement Track 1 Quota vs Track 2 BYOK spend breakdown**
- [ ] **Step 5: Run `go test ./ee/orchestrator/...` and verify pass**

---

## Phase 5: Super Admin Governance & Fleet Telemetry

### Task 6: Implement Global User Search & Fleet Daemon Metrics
**Files:**
- Modify: `ee/auth/admin.go`
- Create: `ee/provisioner/fleet_api.go`
- Test: `ee/auth/admin_test.go`, `ee/provisioner/fleet_api_test.go`

- [ ] **Step 1: Write tests for `GET /admin/users` with pagination and search**
- [ ] **Step 2: Write tests for `POST /admin/orgs/{id}/plan` and `POST /admin/orgs/{id}/grant`**
- [ ] **Step 3: Write tests for `GET /admin/metrics/fleet` and `POST /api/v1/fleet/runners/{id}/drain`**
- [ ] **Step 4: Implement handlers with strict `isAdminAuthorized` super-admin validation**
- [ ] **Step 5: Run `go test ./ee/auth/... ./ee/provisioner/...` and verify pass**

---

## Verification & Handoff Checklist

1. [ ] `gofmt -l cmd/ pkg/ ee/` returns 0 modified files.
2. [ ] `go test ./pkg/...` passes 100%.
3. [ ] `go test ./ee/...` passes 100%.
4. [ ] All existing endpoints maintain exact JSON response shapes for backward compatibility.
