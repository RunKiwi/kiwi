# Progress Ledger: Sandbox & Fleet Telemetry Backend Plan

**Plan:** `docs/superpowers/plans/2026-08-22-sandbox-fleet-telemetry-backend-plan.md`
**Worktree:** `/Users/karn/Desktop/workspace/steelwing/.worktrees/plan-b`
**Branch:** `feat/sandbox-fleet-telemetry`
**Status:** In Progress

---

## Tasks Overview

| Task | Description | Status | Implementer | Reviewer | Commit |
|------|-------------|--------|-------------|----------|--------|
| Task 1 | Export cache stats from `pkg/gitcache` | Completed | task_implementer | task_reviewer | `9e8ea25406c9` |
| Task 2 | Read sandbox memory pressure per running container | Completed | task_implementer | task_reviewer | `2a69782b6b8d` |
| Task 3 | Report cache and memory stats on heartbeat | Completed | task_implementer | task_reviewer | `4d611b8f6783` |
| Task 4 | Store latest heartbeat metrics on `store.Daemon` | In Progress | task_implementer | - | - |
| Task 5 | `GET /api/v1/sandbox/cache/stats` and `GET /admin/metrics/fleet` | Pending | - | - | - |

---

## Task Details & Progress

### Task 1: Export cache stats from `pkg/gitcache`
- [x] Implementer: Write failing tests in `pkg/gitcache/stats_test.go`
- [x] Implementer: Verify failure
- [x] Implementer: Implement `CacheStats` and `Cache.Stats()` in `pkg/gitcache/cache.go`
- [x] Implementer: Verify passing tests, run `gofmt -w pkg/gitcache/`, commit (`9e8ea25406c999658061060f75eb3f7a8bf65f02`)
- [x] Reviewer: Review implementation against spec/plan (Approved)
- [x] Mark complete

### Task 2: Read sandbox memory pressure per running container
- [x] Implementer: Write failing tests in `pkg/daemon/sandbox_stats_test.go`
- [x] Implementer: Verify failure
- [x] Implementer: Implement `ContainerMemStats`, `currentSandboxMemStats`, and parser in `pkg/daemon/sandbox_stats.go`
- [x] Implementer: Verify passing tests, run `gofmt -w pkg/daemon/`, commit (`2a69782b6b8d214db1386a8bdb2f3b564e28d37b`)
- [x] Reviewer: Review implementation against spec/plan (Approved)
- [x] Mark complete

### Task 3: Report cache and memory stats on heartbeat
- [x] Implementer: Write failing tests in `pkg/daemon/heartbeat_stats_test.go`
- [x] Implementer: Verify failure
- [x] Implementer: Update `HeartbeatReq` and heartbeat construction in `pkg/daemon/types.go` & `pkg/daemon/daemon.go`
- [x] Implementer: Verify passing tests, run `gofmt -w pkg/daemon/`, commit (`4d611b8f6783`)
- [x] Reviewer: Review implementation against spec/plan (Approved)
- [x] Mark complete

### Task 4: Store latest heartbeat metrics on `store.Daemon`
- [x] Implementer: Write failing tests in `pkg/store/daemon_telemetry_test.go`
- [x] Implementer: Verify failure
- [x] Implementer: Add models in `pkg/store/daemon_models.go`, migration in `migrations/0047_daemon_telemetry.up.sql`, methods in `pkg/store/store.go` & `pkg/store/daemons.go`, update `ee/orchestrator/daemon_api.go`
- [x] Implementer: Verify passing tests in `pkg/store` and `ee/orchestrator`, run `gofmt -w`, commit
- [ ] Reviewer: Review implementation against spec/plan
- [ ] Mark complete

### Task 5: `GET /api/v1/sandbox/cache/stats` and `GET /admin/metrics/fleet`
- [ ] Implementer: Write failing tests in `ee/orchestrator/telemetry_api_test.go` and `ee/auth/admin_fleet_test.go`
- [ ] Implementer: Verify failure
- [ ] Implementer: Implement `ee/orchestrator/telemetry_api.go`, wire in `ee/orchestrator/server.go` and `ee/auth/admin.go`
- [ ] Implementer: Verify passing tests, run `gofmt -w`, commit
- [ ] Reviewer: Review implementation against spec/plan
- [ ] Mark complete

---

## Final Verification Checklist
- [ ] `gofmt -l cmd/ pkg/ ee/` returns 0 modified files
- [ ] `CGO_ENABLED=0 go vet ./...` succeeds
- [ ] `GIT_CONFIG_GLOBAL=/dev/null CGO_ENABLED=0 go test ./pkg/... ./ee/...` passes 100%
- [ ] `CGO_ENABLED=0 go build ./...` succeeds
- [ ] Licensing boundaries verified (Apache-2.0 packages do not import `ee/`)
- [ ] Backward compatibility verified: nil/absent stats are omitted from calculations, not treated as 0% hit rate
