# Sandbox & Fleet Telemetry Backend Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Serve gitcache hit-rate, gVisor container memory pressure, and per-runner fleet capacity to the Control Plane's dashboard API.

**Architecture:** These are all facts that live in the daemon process — in BYOC, on the customer's own machine. The Control Plane has no way to know them except what the daemon reports. There is exactly one channel from daemon to Control Plane today: the signed `POST /api/v1/daemon/heartbeat` (`ee/orchestrator/daemon_api.go:172`, `pkg/daemon/types.go:36` `HeartbeatReq`). This plan adds optional metrics fields to that request, stores the latest snapshot on the existing `store.Daemon` row (already one row per runner, already touched on every heartbeat), and adds two read endpoints that aggregate across an org's daemons. No new table, no new daemon-to-CP channel, no `pkg/sandbox` HTTP layer (it has none — see spec §0).

**Tech Stack:** Go 1.25, GORM/PostgreSQL.

**Spec:** `docs/superpowers/specs/2026-08-22-platform-overhaul-backend-spec.md` (read §0 Reconciliation first)

## Global Constraints

- `gofmt -l cmd/ pkg/ ee/` must print nothing; `go test ./pkg/...` and `go test ./ee/...` must pass before every commit.
- `HeartbeatReq` is a **signed** body — new fields ride inside the existing signature with no protocol change. But an older daemon binary simply won't send them, so every new field is optional (a pointer or a zero-value-means-absent numeric) and the aggregation endpoints must treat "absent" differently from "zero." State this per task; don't let a nil default silently read as "0% cache hit rate."
- `pkg/gitcache` and `pkg/daemon` are Apache-2.0; nothing added there may import `ee/`.
- Next migration number is `0047` (Plan A claims `0046`; if Plan A hasn't landed yet when this runs, check `ls migrations/` and use whatever is actually next).

---

## Phase 1: Daemon-Side Instrumentation (Apache-2.0)

### Task 1: Export cache stats from `pkg/gitcache`

**Files:**
- Modify: `pkg/gitcache/cache.go` (the `Cache` struct and its internal accounting, currently using unexported `recordAccess`/active-count bookkeeping around lines 278-320)
- Test: `pkg/gitcache/stats_test.go`

**Interfaces:**
- Produces: `type CacheStats struct { TotalRepos int; TotalActiveWorktrees int; HitCount int64; MissCount int64 }` and `func (c *Cache) Stats() CacheStats` — consumed by Task 2 (`pkg/daemon`).

`Cache` already tracks per-repo access internally (`recordAccess`, `c.repos` or equivalent map — read `cache.go` in full before this task to find the exact field names holding per-repo state; the plan above found `recordAccess`/`recordActive`/`pickVictim`/`stillEvictable` but not the struct's field names, which this task's implementer must read directly). Do not introduce a second bookkeeping structure — `Stats()` should read the same state those functions already maintain.

- [ ] **Step 1: Read `pkg/gitcache/cache.go` in full and note the exact struct holding per-repo entries and their fields (last-access time, active worktree count).**

This step has no test of its own — it's the investigation `Stats()`'s test in Step 2 depends on.

- [ ] **Step 2: Write a failing test**

```go
package gitcache

import "testing"

func TestCacheStatsReflectsRepoCount(t *testing.T) {
	c, cleanup := setupTestCache(t) // existing helper from cache_test.go
	defer cleanup()

	repo := makeLocalRepo(t, "stats-repo") // existing helper from eviction_test.go
	getAndRelease(t, c, repo)              // existing helper from eviction_test.go

	stats := c.Stats()
	if stats.TotalRepos != 1 {
		t.Fatalf("TotalRepos = %d, want 1", stats.TotalRepos)
	}
	if stats.TotalActiveWorktrees != 0 {
		t.Fatalf("TotalActiveWorktrees = %d, want 0 (released)", stats.TotalActiveWorktrees)
	}
}

func TestCacheStatsCountsHitsAndMisses(t *testing.T) {
	c, cleanup := setupTestCache(t)
	defer cleanup()
	repo := makeLocalRepo(t, "hit-repo")

	getAndRelease(t, c, repo) // first fetch: a miss (repo not yet cached)
	getAndRelease(t, c, repo) // second fetch: a hit (bare clone already present)

	stats := c.Stats()
	if stats.MissCount != 1 || stats.HitCount != 1 {
		t.Fatalf("HitCount=%d MissCount=%d, want 1/1", stats.HitCount, stats.MissCount)
	}
}
```

If `getAndRelease` doesn't distinguish hit from miss internally today, this second test may require `Stats()` to derive hit/miss from whether `GetWorktree`'s bare-clone step actually ran `git clone` vs. reused an existing bare repo — check `cache.go`'s `GetWorktree` (line 153) for where that distinction is already observable (it must be, since the eviction tests already assert "recently used" vs "cold" repos), and count there rather than adding a new counter that can drift from the real fetch path.

- [ ] **Step 3: Run, confirm it fails to compile**

Run: `go test ./pkg/gitcache/... -run TestCacheStats -v`
Expected: FAIL — `c.Stats undefined`

- [ ] **Step 4: Implement `CacheStats` and `Stats()`**

Add near the top of `cache.go`, next to the `Cache` struct definition:

```go
// CacheStats is a point-in-time summary of the cache's contents, used to
// report gitcache health to the Control Plane over the daemon heartbeat.
type CacheStats struct {
	TotalRepos           int
	TotalActiveWorktrees int
	HitCount             int64
	MissCount             int64
}

// Stats reports the cache's current contents. It takes the same lock the
// eviction and access-recording paths already take, so a stats read never
// races a concurrent GetWorktree/RemoveWorktree.
func (c *Cache) Stats() CacheStats {
	c.mu.Lock() // use whatever the existing mutex field is actually named — read from cache.go
	defer c.mu.Unlock()
	var active int
	for _, e := range c.repos { // substitute the real field name found in Step 1
		active += e.activeCount // substitute the real field name found in Step 1
	}
	return CacheStats{
		TotalRepos:           len(c.repos),
		TotalActiveWorktrees: active,
		HitCount:             c.hitCount,
		MissCount:            c.missCount,
	}
}
```

Add `hitCount`/`missCount int64` fields to `Cache` (guarded by the same mutex) and increment them at the exact point in `GetWorktree` where the code already knows whether the bare clone was freshly created or reused (this is the investigation from Step 1 — do not guess the line number here).

- [ ] **Step 5: Run the tests, confirm pass**

Run: `go test ./pkg/gitcache/... -v`
Expected: PASS (full package, to confirm the new counters don't break existing concurrency tests like `TestCache_ConcurrentGetWithEviction`)

- [ ] **Step 6: `gofmt -w pkg/gitcache/` and commit**

```bash
git add pkg/gitcache/cache.go pkg/gitcache/stats_test.go
git commit -m "feat(gitcache): export cache stats for heartbeat reporting"
```

---

### Task 2: Read sandbox memory pressure per running container

**Files:**
- Create: `pkg/daemon/sandbox_stats.go`
- Test: `pkg/daemon/sandbox_stats_test.go`

**Interfaces:**
- Consumes: nothing new (reads `docker stats`/cgroup files for containers this daemon started).
- Produces: `type ContainerMemStats struct { ContainerID string; RSSMB int64; LimitMB int64 }` and `func currentSandboxMemStats(ctx context.Context) ([]ContainerMemStats, error)` — consumed by Task 3.

`pkg/daemon/sandbox_memory.go` (existing) computes the memory *limit* a sandbox is launched with (`sandboxMemoryLimit()`); it has no runtime reader for *current usage*. This task adds that reader, following the same "shell out to `docker`, parse plain text, fail soft" pattern `sandbox_memory.go` already uses for `/proc/meminfo` — do not add a Docker SDK dependency for this.

- [ ] **Step 1: Write a failing test using a fake command runner**

Match the existing pattern in `sandbox_memory.go` of a small pure function taking file/command output as a string, tested without invoking real `docker`:

```go
package daemon

import "testing"

func TestParseDockerStatsOutput(t *testing.T) {
	// docker stats --no-stream --format "{{.ID}} {{.MemUsage}}" output looks like:
	// "a1b2c3d4e5f6 512.3MiB / 4GiB"
	out := "a1b2c3d4e5f6 512.3MiB / 4GiB\nb2c3d4e5f6a7 1.2GiB / 4GiB\n"
	stats, err := parseDockerStatsOutput(out)
	if err != nil {
		t.Fatalf("parseDockerStatsOutput: %v", err)
	}
	if len(stats) != 2 {
		t.Fatalf("got %d entries, want 2", len(stats))
	}
	if stats[0].ContainerID != "a1b2c3d4e5f6" || stats[0].LimitMB != 4096 {
		t.Fatalf("stats[0] = %+v", stats[0])
	}
}
```

- [ ] **Step 2: Run, confirm it fails**

Run: `go test ./pkg/daemon/... -run TestParseDockerStatsOutput -v`
Expected: FAIL — `parseDockerStatsOutput undefined`

- [ ] **Step 3: Implement**

```go
// SPDX-License-Identifier: Apache-2.0
package daemon

import (
	"context"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// ContainerMemStats is one running sandbox's memory usage, reported to the
// Control Plane over the heartbeat so gVisor OOM risk is visible before it
// kills a run (see sandbox_memory.go's doc comment for why that matters).
type ContainerMemStats struct {
	ContainerID string
	RSSMB       int64
	LimitMB     int64
}

// currentSandboxMemStats shells out to `docker stats`, the same tool
// sandbox_memory.go's neighbor package already assumes is present (Docker is
// a hard dependency of this daemon regardless of the runsc/runc runtime
// underneath it). A failure here is reported to the caller, which treats it
// as "no data this heartbeat" rather than fatal — matching hostMemoryBytes's
// fail-soft convention in sandbox_memory.go.
func currentSandboxMemStats(ctx context.Context) ([]ContainerMemStats, error) {
	cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(cctx, "docker", "stats", "--no-stream", "--format", "{{.ID}} {{.MemUsage}}").Output()
	if err != nil {
		return nil, fmt.Errorf("docker stats: %w", err)
	}
	return parseDockerStatsOutput(string(out))
}

func parseDockerStatsOutput(out string) ([]ContainerMemStats, error) {
	var stats []ContainerMemStats
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if line == "" {
			continue
		}
		fields := strings.SplitN(line, " ", 2)
		if len(fields) != 2 {
			continue
		}
		usage := strings.Split(fields[1], "/")
		if len(usage) != 2 {
			continue
		}
		rss, err := parseDockerMemSize(strings.TrimSpace(usage[0]))
		if err != nil {
			continue
		}
		limit, err := parseDockerMemSize(strings.TrimSpace(usage[1]))
		if err != nil {
			continue
		}
		stats = append(stats, ContainerMemStats{ContainerID: fields[0], RSSMB: rss, LimitMB: limit})
	}
	return stats, nil
}

// parseDockerMemSize parses docker's human-readable size ("512.3MiB", "4GiB")
// into whole megabytes.
func parseDockerMemSize(s string) (int64, error) {
	var mult float64 = 1
	switch {
	case strings.HasSuffix(s, "GiB"):
		mult = 1024
		s = strings.TrimSuffix(s, "GiB")
	case strings.HasSuffix(s, "MiB"):
		s = strings.TrimSuffix(s, "MiB")
	case strings.HasSuffix(s, "KiB"):
		mult = 1.0 / 1024
		s = strings.TrimSuffix(s, "KiB")
	default:
		return 0, fmt.Errorf("unrecognized docker memory unit in %q", s)
	}
	v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return 0, err
	}
	return int64(v * mult), nil
}
```

- [ ] **Step 4: Run the test, confirm it passes**

Run: `go test ./pkg/daemon/... -run TestParseDockerStatsOutput -v`
Expected: PASS

- [ ] **Step 5: Add a test for the failure path (docker not present / errors)**

```go
func TestCurrentSandboxMemStatsFailsSoft(t *testing.T) {
	_, err := currentSandboxMemStats(context.Background())
	// On a CI box without a running container this may succeed with an empty
	// slice or fail if `docker` isn't on PATH — either is acceptable; the
	// only thing this test pins down is that it never panics and always
	// returns a wrapped error rather than an untyped one, so the heartbeat
	// caller (Task 3) can log-and-continue.
	if err != nil && !strings.Contains(err.Error(), "docker stats") {
		t.Fatalf("unexpected error shape: %v", err)
	}
}
```

- [ ] **Step 6: Run the full daemon suite, `gofmt -w`, commit**

```bash
go test ./pkg/daemon/... -v
gofmt -w pkg/daemon/
git add pkg/daemon/sandbox_stats.go pkg/daemon/sandbox_stats_test.go
git commit -m "feat(daemon): read per-container memory usage via docker stats"
```

---

### Task 3: Report cache and memory stats on heartbeat

**Files:**
- Modify: `pkg/daemon/types.go` (`HeartbeatReq`, lines 36-43)
- Modify: `pkg/daemon/client.go` (wherever `HeartbeatReq` is populated before signing — find the call site with `grep -n "HeartbeatReq{" pkg/daemon/*.go`)
- Test: `pkg/daemon/heartbeat_stats_test.go`

**Interfaces:**
- Consumes: `Cache.Stats()` (Task 1), `currentSandboxMemStats` (Task 2).
- Produces: `HeartbeatReq.CacheStats *CacheHeartbeatStats`, `HeartbeatReq.MemStats []ContainerMemStats` — consumed by Task 4 (`ee/orchestrator`).

- [ ] **Step 1: Find the heartbeat construction site**

Run: `grep -n "HeartbeatReq{" pkg/daemon/*.go`

Read that function in full — it's the one place per-heartbeat data gets assembled before `client.Heartbeat` signs and sends it.

- [ ] **Step 2: Write a failing test asserting the populated request carries stats**

```go
func TestBuildHeartbeatIncludesCacheAndMemStats(t *testing.T) {
	// Construct whatever the daemon struct under test needs (a *Daemon with
	// a real *gitcache.Cache pointed at a temp dir, per this package's
	// existing test setup) and call the exact function found in Step 1.
	req := d.buildHeartbeatReq(context.Background()) // substitute the real function name
	if req.CacheStats == nil {
		t.Fatal("CacheStats should be populated when the daemon has a cache")
	}
}
```

- [ ] **Step 3: Run, confirm it fails**

Run: `go test ./pkg/daemon/... -run TestBuildHeartbeatIncludesCacheAndMemStats -v`

- [ ] **Step 4: Add the fields and populate them**

In `pkg/daemon/types.go`, add to `HeartbeatReq` after `Timestamp`:

```go
	// CacheStats and MemStats are best-effort telemetry, omitted (nil/empty)
	// rather than zero-valued when unavailable — an org-wide aggregate must
	// be able to tell "no data this heartbeat" from "0% hit rate."
	CacheStats *CacheHeartbeatStats `json:"cache_stats,omitempty"`
	MemStats   []ContainerMemStats  `json:"mem_stats,omitempty"`
```

```go
// CacheHeartbeatStats mirrors gitcache.CacheStats without pkg/daemon
// depending on pkg/gitcache's internal Cache type directly in the wire
// struct — keeps the JSON contract stable if gitcache's internals change.
type CacheHeartbeatStats struct {
	TotalRepos           int   `json:"total_repos"`
	TotalActiveWorktrees int   `json:"total_active_worktrees"`
	HitCount              int64 `json:"hit_count"`
	MissCount             int64 `json:"miss_count"`
}
```

At the heartbeat construction site found in Step 1, populate both, logging and leaving them nil/empty on failure rather than aborting the heartbeat (a heartbeat must still land even if `docker stats` is slow or the daemon has no cache configured):

```go
	if d.gitCache != nil { // substitute the daemon's actual field name
		gs := d.gitCache.Stats()
		req.CacheStats = &CacheHeartbeatStats{
			TotalRepos: gs.TotalRepos, TotalActiveWorktrees: gs.TotalActiveWorktrees,
			HitCount: gs.HitCount, MissCount: gs.MissCount,
		}
	}
	if mem, err := currentSandboxMemStats(ctx); err != nil {
		log.Printf("[daemon] heartbeat: could not read sandbox memory stats: %v", err)
	} else {
		req.MemStats = mem
	}
```

- [ ] **Step 5: Run the test, confirm it passes**

Run: `go test ./pkg/daemon/... -run TestBuildHeartbeatIncludesCacheAndMemStats -v`
Expected: PASS

- [ ] **Step 6: Run the full daemon suite**

Run: `go test ./pkg/daemon/... -v`
Expected: PASS — confirm no existing heartbeat test asserted an exact JSON shape that the two new `omitempty` fields would break.

- [ ] **Step 7: `gofmt -w pkg/daemon/` and commit**

```bash
git add pkg/daemon/types.go pkg/daemon/client.go pkg/daemon/heartbeat_stats_test.go
git commit -m "feat(daemon): report cache and sandbox memory stats on heartbeat"
```

---

## Phase 2: Control Plane Storage & Endpoints (ee/, pkg/store)

### Task 4: Store the latest heartbeat metrics on `store.Daemon`

**Files:**
- Modify: `pkg/store/daemon_models.go` (`Daemon` struct, lines 20-38)
- Create: `migrations/0047_daemon_telemetry.up.sql` (renumber if Plan A's `0046` hasn't landed — check `ls migrations/` first)
- Modify: `pkg/store/store.go` and `pkg/store/postgres.go` (`TouchDaemon` — widen it, or add a sibling method)
- Modify: `ee/orchestrator/daemon_api.go` (`handleDaemonHeartbeat`, where `s.storage.TouchDaemon` is called, line ~217)
- Test: `pkg/store/daemon_telemetry_test.go`

**Interfaces:**
- Consumes: nothing external.
- Produces: `Daemon.LastCacheStats`, `Daemon.LastMemStatsJSON`, `Daemon.ActiveContainers`; `Store.UpdateDaemonTelemetry(ctx, daemonID string, cache *CacheHeartbeatStats, mem []ContainerMemStats) error` — consumed by Task 5.

Extending `Daemon` rather than adding a `RunnerNode` table (spec §0): one row already exists per runner and is already touched every heartbeat.

- [ ] **Step 1: Write a failing test**

```go
package store

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUpdateDaemonTelemetry(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	d, err := s.RegisterDaemon(ctx, mustJoinToken(t, s, "org-1"), "sign-pub-1", "enc-pub-1")
	require.NoError(t, err)

	err = s.UpdateDaemonTelemetry(ctx, d.ID, &CacheHeartbeatStats{
		TotalRepos: 18, TotalActiveWorktrees: 2, HitCount: 94, MissCount: 6,
	}, []ContainerMemStats{{ContainerID: "c-01", RSSMB: 1024, LimitMB: 4096}})
	require.NoError(t, err)

	got, err := s.GetDaemonBySignPubKey(ctx, "sign-pub-1")
	require.NoError(t, err)
	require.Equal(t, 18, got.LastCacheStats.TotalRepos)
	require.Equal(t, 1, got.ActiveContainers)
}
```

(`mustJoinToken` should already exist as a helper in this package's daemon tests — check `pkg/store/daemon_test.go` before writing a new one. `CacheHeartbeatStats`/`ContainerMemStats` here are re-declared in `pkg/store` as plain data types with no dependency on `pkg/daemon` — `pkg/store` must not import `pkg/daemon` — mirror the JSON shape, don't import the type.)

- [ ] **Step 2: Run, confirm it fails**

Run: `go test ./pkg/store/... -run TestUpdateDaemonTelemetry -v`
Expected: FAIL — `s.UpdateDaemonTelemetry undefined`

- [ ] **Step 3: Add the columns, types, and method**

In `pkg/store/daemon_models.go`, define locally (do not import `pkg/daemon`):

```go
// CacheHeartbeatStats and ContainerMemStats mirror the JSON shape
// pkg/daemon.HeartbeatReq sends — duplicated rather than imported because
// pkg/store must not depend on pkg/daemon (see pkg/licensing_boundary_test.go
// for the enforced direction; this isn't a licensing boundary but the same
// "one-way dependency" discipline applies to avoid an import cycle, since
// pkg/daemon already depends on pkg/store).
type CacheHeartbeatStats struct {
	TotalRepos           int   `json:"total_repos"`
	TotalActiveWorktrees int   `json:"total_active_worktrees"`
	HitCount              int64 `json:"hit_count"`
	MissCount             int64 `json:"miss_count"`
}

type ContainerMemStats struct {
	ContainerID string `json:"container_id"`
	RSSMB       int64  `json:"rss_mb"`
	LimitMB     int64  `json:"limit_mb"`
}
```

Add to `Daemon` (after `LastSeenAt`, line 34):

```go
	// LastCacheStats and LastMemStats are the most recent heartbeat's
	// telemetry, nil until the daemon binary is new enough to report them.
	LastCacheStats *CacheHeartbeatStats `gorm:"type:jsonb;serializer:json" json:"last_cache_stats,omitempty"`
	LastMemStats   []ContainerMemStats  `gorm:"type:jsonb;serializer:json" json:"last_mem_stats,omitempty"`
	// ActiveContainers is len(LastMemStats), denormalized so a fleet-capacity
	// query doesn't need to unmarshal the jsonb column just to count it.
	ActiveContainers int `gorm:"not null;default:0" json:"active_containers"`
```

Migration:

```sql
-- migrations/0047_daemon_telemetry.up.sql (renumber if 0046 is already taken)
ALTER TABLE daemons ADD COLUMN IF NOT EXISTS last_cache_stats JSONB;
ALTER TABLE daemons ADD COLUMN IF NOT EXISTS last_mem_stats JSONB;
ALTER TABLE daemons ADD COLUMN IF NOT EXISTS active_containers INTEGER NOT NULL DEFAULT 0;
```

Store interface (`pkg/store/store.go`, near `TouchDaemon`):

```go
	// UpdateDaemonTelemetry records the latest heartbeat's cache and memory
	// stats. Called alongside TouchDaemon, not instead of it — liveness and
	// telemetry are tracked separately since an older daemon updates the
	// former without ever calling this.
	UpdateDaemonTelemetry(ctx context.Context, daemonID string, cache *CacheHeartbeatStats, mem []ContainerMemStats) error
```

Implementation (`pkg/store/postgres.go`, near `TouchDaemon`):

```go
func (s *PostgresStore) UpdateDaemonTelemetry(ctx context.Context, daemonID string, cache *CacheHeartbeatStats, mem []ContainerMemStats) error {
	if cache == nil && mem == nil {
		return nil
	}
	updates := map[string]interface{}{}
	if cache != nil {
		updates["last_cache_stats"] = cache
	}
	if mem != nil {
		updates["last_mem_stats"] = mem
		updates["active_containers"] = len(mem)
	}
	return s.db.WithContext(ctx).Model(&Daemon{}).Where("id = ?", daemonID).Updates(updates).Error
}
```

In `ee/orchestrator/daemon_api.go`, right after the existing `s.storage.TouchDaemon(r.Context(), d.ID)` call (line ~217):

```go
	if req.CacheStats != nil || req.MemStats != nil {
		var cache *store.CacheHeartbeatStats
		if req.CacheStats != nil {
			cache = &store.CacheHeartbeatStats{
				TotalRepos: req.CacheStats.TotalRepos, TotalActiveWorktrees: req.CacheStats.TotalActiveWorktrees,
				HitCount: req.CacheStats.HitCount, MissCount: req.CacheStats.MissCount,
			}
		}
		var mem []store.ContainerMemStats
		for _, m := range req.MemStats {
			mem = append(mem, store.ContainerMemStats{ContainerID: m.ContainerID, RSSMB: m.RSSMB, LimitMB: m.LimitMB})
		}
		if err := s.storage.UpdateDaemonTelemetry(r.Context(), d.ID, cache, mem); err != nil {
			log.Printf("[daemon] telemetry update for %s: %v", d.ID, err)
		}
	}
```

(This is a manual field-by-field copy between `pkg/daemon.CacheHeartbeatStats` and `store.CacheHeartbeatStats` — deliberate, since `ee/orchestrator` is the one place both types can be imported together without creating a cycle.)

- [ ] **Step 4: Run the store test, confirm it passes**

Run: `go test ./pkg/store/... -run TestUpdateDaemonTelemetry -v`
Expected: PASS

- [ ] **Step 5: Add an `ee/orchestrator` test for the heartbeat handler forwarding telemetry**

Follow the existing `handleDaemonHeartbeat` test pattern in `ee/orchestrator/daemon_api_test.go` (signed request helpers already exist there per Plan A's Task 6):

```go
func TestHeartbeatStoresDaemonTelemetry(t *testing.T) {
	// ... existing scaffolding: register a daemon, post a signed HeartbeatReq
	// with CacheStats and MemStats populated.

	got, err := testStore.GetDaemonBySignPubKey(context.Background(), signPubKeyB64)
	require.NoError(t, err)
	require.NotNil(t, got.LastCacheStats)
	require.Equal(t, 1, got.ActiveContainers)
}
```

- [ ] **Step 6: Run, confirm pass; run full suites**

Run: `go test ./pkg/store/... ./ee/orchestrator/... -v`
Expected: PASS

- [ ] **Step 7: `gofmt -w`, commit**

```bash
gofmt -w pkg/store/ ee/orchestrator/
git add pkg/store/daemon_models.go pkg/store/store.go pkg/store/postgres.go pkg/store/daemon_telemetry_test.go migrations/0047_daemon_telemetry.up.sql ee/orchestrator/daemon_api.go
git commit -m "feat(store): persist daemon cache and memory telemetry from heartbeat"
```

---

### Task 5: `GET /api/v1/sandbox/cache/stats` and `GET /admin/metrics/fleet`

**Files:**
- Create: `ee/orchestrator/telemetry_api.go`
- Modify: `ee/orchestrator/server.go` (register the new route)
- Modify: `ee/auth/admin.go` (register `/admin/metrics/fleet`)
- Test: `ee/orchestrator/telemetry_api_test.go`, `ee/auth/admin_fleet_test.go`

**Interfaces:**
- Consumes: `Store.ListDaemons(ctx, orgID)` (existing).
- Produces: `GET /api/v1/sandbox/cache/stats` (org-scoped, aggregates the caller's daemons) and `GET /admin/metrics/fleet` (super-admin, aggregates every daemon) — response shapes per spec §3 Groups B and E, adjusted to what's actually available (no `bandwidth_saved_mb`/`avg_clone_latency_ms` — nothing in this plan or the daemon reports clone latency; omit those fields rather than fabricate them, or add clone-latency timing to `pkg/gitcache.GetWorktree` as a follow-up if the frontend needs it — flag this gap rather than inventing a number).

- [ ] **Step 1: Write failing tests for both endpoints**

```go
// ee/orchestrator/telemetry_api_test.go
func TestHandleSandboxCacheStats(t *testing.T) {
	s, mux := newTestServer(t)
	seedDaemonWithTelemetry(t, s, "org-1", &store.CacheHeartbeatStats{
		TotalRepos: 18, TotalActiveWorktrees: 2, HitCount: 94, MissCount: 6,
	}, nil)

	req := authedRequest(t, http.MethodGet, "/api/v1/sandbox/cache/stats", nil, "org-1")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var resp struct {
		CacheHitRatePct   float64 `json:"cache_hit_rate_pct"`
		TotalCachedTrees  int     `json:"total_cached_trees"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.InDelta(t, 94.0, resp.CacheHitRatePct, 0.1) // 94/(94+6)*100
	require.Equal(t, 18, resp.TotalCachedTrees)
}
```

```go
// ee/auth/admin_fleet_test.go
func TestHandleAdminMetricsFleetRequiresSuperAdmin(t *testing.T) {
	db := newTestDB(t)
	mux := http.NewServeMux()
	AdminRouter(db, mux)

	req := httptest.NewRequest(http.MethodGet, "/admin/metrics/fleet", nil) // no auth
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	require.Equal(t, http.StatusForbidden, w.Code)
}

func TestHandleAdminMetricsFleetAggregates(t *testing.T) {
	db := newTestDB(t)
	seedDaemonWithTelemetryDirect(t, db, "org-1", 4, 8) // active, max helper writing directly via GORM
	mux := http.NewServeMux()
	AdminRouter(db, mux)

	req := httptest.NewRequest(http.MethodGet, "/admin/metrics/fleet", nil).WithContext(ctxWithServerToken(t)) // existing pattern from admin_test.go
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code)
}
```

- [ ] **Step 2: Run, confirm both fail (404)**

Run: `go test ./ee/orchestrator/... -run TestHandleSandboxCacheStats -v`
Run: `go test ./ee/auth/... -run TestHandleAdminMetricsFleet -v`

- [ ] **Step 3: Implement `GET /api/v1/sandbox/cache/stats`**

```go
// SPDX-License-Identifier: LicenseRef-Kiwi-BSL-1.1
// Copyright (c) 2026 RunKiwi. Licensed under the Business Source License 1.1.
// See ee/LICENSE. This is Control Plane code and is NOT Apache-2.0.

package orchestrator

import (
	"net/http"

	"github.com/ibreakthecloud/kiwi/ee/auth"
)

// handleSandboxCacheStats serves GET /api/v1/sandbox/cache/stats, aggregating
// the caller's org's daemons' most recent heartbeat cache telemetry. A daemon
// that has never reported (older binary, or hasn't heartbeat since upgrade)
// contributes nothing rather than a fabricated zero.
func (s *Server) handleSandboxCacheStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	claims := auth.ClaimsFromContext(r.Context())
	if claims == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	daemons, err := s.storage.ListDaemons(r.Context(), claims.OrgID)
	if err != nil {
		http.Error(w, "failed to list daemons", http.StatusInternalServerError)
		return
	}

	var totalRepos, totalActive int
	var hits, misses int64
	var reporting int
	for _, d := range daemons {
		if d.LastCacheStats == nil {
			continue
		}
		reporting++
		totalRepos += d.LastCacheStats.TotalRepos
		totalActive += d.LastCacheStats.TotalActiveWorktrees
		hits += d.LastCacheStats.HitCount
		misses += d.LastCacheStats.MissCount
	}

	hitRate := 0.0
	if hits+misses > 0 {
		hitRate = float64(hits) / float64(hits+misses) * 100
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"cache_hit_rate_pct":    hitRate,
		"total_cached_trees":    totalRepos,
		"total_active_worktrees": totalActive,
		"daemons_reporting":     reporting,
		"daemons_total":         len(daemons),
	})
}
```

Register in `ee/orchestrator/server.go`, near the other `/api/v1/` routes (line ~483, alongside `/api/v1/integrations`):

```go
	mux.HandleFunc("/api/v1/sandbox/cache/stats", s.handleSandboxCacheStats)
```

- [ ] **Step 4: Implement `GET /admin/metrics/fleet`**

Add to `ee/auth/admin.go`'s `AdminRouter`, alongside the `/admin/stats` registration (line 34):

```go
	mux.HandleFunc("/admin/metrics/fleet", func(w http.ResponseWriter, r *http.Request) {
		if !isAdminAuthorized(r) {
			http.Error(w, "Forbidden: admin access required", http.StatusForbidden)
			return
		}
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		handleAdminMetricsFleet(db, w, r)
	})
```

```go
func handleAdminMetricsFleet(db *gorm.DB, w http.ResponseWriter, r *http.Request) {
	var daemons []store.Daemon // add "github.com/ibreakthecloud/kiwi/pkg/store" import if not already present
	if err := db.Find(&daemons).Error; err != nil {
		http.Error(w, "Failed to load fleet metrics", http.StatusInternalServerError)
		return
	}
	var activeContainers int
	for _, d := range daemons {
		activeContainers += d.ActiveContainers
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"total_daemons":     len(daemons),
		"active_containers": activeContainers,
	})
}
```

(This is deliberately smaller than spec §3 Group E's example response — `host_pool`, `queue_depth`, `avg_cold_start_ms`, and `imds_blocked_count` have no data source anywhere in this codebase today. Report what's real; do not invent placeholder values for the rest. If the frontend needs `queue_depth`, that's a count of `QUEUED` `queued_tasks` rows, addable in a follow-up task with its own test — flag it, don't silently add a fabricated field here.)

- [ ] **Step 5: Run both tests, confirm pass**

Run: `go test ./ee/orchestrator/... -run TestHandleSandboxCacheStats -v`
Run: `go test ./ee/auth/... -run TestHandleAdminMetricsFleet -v`
Expected: PASS

- [ ] **Step 6: Full suites, gofmt, commit**

```bash
go test ./ee/orchestrator/... ./ee/auth/... -v
gofmt -w ee/orchestrator/ ee/auth/
git add ee/orchestrator/telemetry_api.go ee/orchestrator/telemetry_api_test.go ee/orchestrator/server.go ee/auth/admin.go ee/auth/admin_fleet_test.go
git commit -m "feat(orchestrator,auth): add org and admin fleet/cache telemetry endpoints"
```

---

## Verification & Handoff Checklist

1. [ ] `gofmt -l cmd/ pkg/ ee/` returns 0 modified files.
2. [ ] `go test ./pkg/...` and `go test ./ee/...` pass 100%.
3. [ ] `go build ./...` succeeds.
4. [ ] A daemon that never sends `CacheStats`/`MemStats` (simulate by omitting them in a heartbeat test) leaves `Daemon.LastCacheStats` nil and is excluded from `cache_hit_rate_pct`'s denominator, not counted as a 0%-hit-rate daemon — re-check this explicitly, it's the backward-compat rule this whole plan depends on.
5. [ ] `GET /api/v1/sandbox/cache/stats` and `GET /admin/metrics/fleet` return real, computed numbers only — no field in either response is a placeholder or a value fabricated from spec §3's example JSON without a genuine source.
