# Spend Quota Ceilings & Admin User Search Backend Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add the two things spec §3 Groups D and E ask for that don't already exist: ceiling context (agent-minute/concurrent-lease limits) on the existing spend endpoint, and cross-tenant user search for Kiwi staff.

**Architecture:** `GET /api/v1/spend` (`ee/orchestrator/spend_api.go`) already reports Track 1 (`Allowance []AllowanceBucket`, sourced from `ee/entitlement`) and Track 2 (`CostUSD`/`ByProvider`, sourced from `Job`/`QueuedTask.Funding`) usage. It's missing the *ceiling* the frontend needs to render a progress bar: how many agent-minutes/concurrent leases the org is capped at, not just how many it's used. This plan adds that from `OrgLimits` and a live lease count — no new endpoint. Separately, `ee/auth/admin.go` has no cross-org user search (only `GET /admin/orgs/{orgID}/users`, scoped to one org); this plan adds `GET /admin/users?search=&limit=&offset=`.

**Tech Stack:** Go 1.25, GORM/PostgreSQL.

**Spec:** `docs/superpowers/specs/2026-08-22-platform-overhaul-backend-spec.md` (read §0 Reconciliation first)

## Global Constraints

- `gofmt -l cmd/ pkg/ ee/` must print nothing; `go test ./pkg/...` and `go test ./ee/...` must pass before every commit.
- Do **not** add `GET /api/v1/usage/breakdown` — it would duplicate `GET /api/v1/spend` (spec §0). Extend `SpendResponse`.
- `GET /admin/users` is super-admin only, gated by `isAdminAuthorized` — the same function every other top-level `/admin/*` route in `ee/auth/admin.go` already uses (line 337).
- No new migration: this plan reads existing columns (`OrgLimits.MaxAgentMinutesPerMonth`, `OrgLimits.MaxConcurrentJobs`) and counts existing rows (`QueuedTask` where `status = 'LEASED'`).

---

## Task 1: Add quota ceilings to `GET /api/v1/spend`

**Files:**
- Modify: `ee/orchestrator/spend_api.go` (`SpendResponse` struct lines 40-77, `handleSpend` lines 91-199)
- Test: `ee/orchestrator/spend_api_test.go`

**Interfaces:**
- Consumes: `Store.GetOrgLimits(ctx, orgID) (*store.OrgLimits, error)` (existing, `pkg/store/store.go:83`); a new count query on `queued_tasks`.
- Produces: `SpendResponse.AgentMinutesLimit float64`, `SpendResponse.ConcurrentLeasesActive int`, `SpendResponse.ConcurrentLeasesMax int` — no new store method needed for the limit (read directly), one small helper for the live count.

- [ ] **Step 1: Write a failing test extending `buildSpend`'s existing test suite**

`buildSpend` (line 214) is pure — it doesn't touch limits or the live lease count, since both come from separate queries `handleSpend` runs itself (see how it already fetches `Allowance` after calling `buildSpend`, lines 136-195). Ceilings follow the same pattern: added to `resp` in `handleSpend` after `buildSpend` returns, not inside `buildSpend`. Write the test at the handler level:

```go
// ee/orchestrator/spend_api_test.go
func TestHandleSpendIncludesQuotaCeilings(t *testing.T) {
	s, mux := newTestServer(t) // existing helper used by this file's other handler-level tests, if any exist; otherwise mirror ee/orchestrator/jobs_api_test.go's server setup
	seedOrgLimits(t, s, "org-1", 500.0, 10) // helper: write an OrgLimits row with MaxAgentMinutesPerMonth=500, MaxConcurrentJobs=10
	seedLeasedTask(t, s, "org-1", "job-1", "t-1") // helper: EnqueueTask + LeaseNextTask, leaving it LEASED

	req := authedRequest(t, http.MethodGet, "/api/v1/spend", nil, "org-1")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var resp SpendResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Equal(t, 500.0, resp.AgentMinutesLimit)
	require.Equal(t, 1, resp.ConcurrentLeasesActive)
	require.Equal(t, 10, resp.ConcurrentLeasesMax)
}
```

If `ee/orchestrator/spend_api_test.go` today only has table-driven tests against `buildSpend` directly (no HTTP-level test yet — check the file first, since the earlier investigation only confirmed `buildSpend(...)` call sites, not whether a `handleSpend`-level test exists), add the HTTP-level scaffolding this test needs (test server + auth helper) by copying the pattern from `ee/orchestrator/jobs_api_test.go` or `ee/orchestrator/daemon_api_test.go`, whichever is closest — don't invent a third pattern.

- [ ] **Step 2: Run, confirm it fails**

Run: `go test ./ee/orchestrator/... -run TestHandleSpendIncludesQuotaCeilings -v`
Expected: FAIL — `resp.AgentMinutesLimit` doesn't exist / is always zero.

- [ ] **Step 3: Add the fields and populate them**

In `SpendResponse` (`spend_api.go:40`), add after `AgentMinutes float64` (line 48):

```go
	// AgentMinutesLimit is the org's monthly ceiling (OrgLimits.MaxAgentMinutesPerMonth);
	// 0 means "no limits row" is genuinely different from "unlimited" — check
	// the org's plan/limits row directly if that distinction matters to the
	// caller, since this field alone can't disambiguate them.
	AgentMinutesLimit      float64 `json:"agent_minutes_limit"`
	ConcurrentLeasesActive int     `json:"concurrent_leases_active"`
	ConcurrentLeasesMax    int     `json:"concurrent_leases_max"`
```

In `handleSpend` (`spend_api.go`), after `resp.Plan = plan` (line 146):

```go
	if limits, lerr := s.storage.GetOrgLimits(r.Context(), claims.OrgID); lerr != nil {
		log.Printf("[spend] loading limits for org %s: %v", claims.OrgID, lerr)
	} else if limits != nil {
		resp.AgentMinutesLimit = limits.MaxAgentMinutesPerMonth
		resp.ConcurrentLeasesMax = limits.MaxConcurrentJobs
	}
	var activeLeases int64
	if err := s.db.WithContext(r.Context()).Model(&store.QueuedTask{}).
		Where("org_id = ? AND status = ?", claims.OrgID, store.TaskLeased).
		Count(&activeLeases).Error; err != nil {
		log.Printf("[spend] counting active leases for org %s: %v", claims.OrgID, err)
	} else {
		resp.ConcurrentLeasesActive = int(activeLeases)
	}
```

- [ ] **Step 4: Run the test, confirm it passes**

Run: `go test ./ee/orchestrator/... -run TestHandleSpendIncludesQuotaCeilings -v`
Expected: PASS

- [ ] **Step 5: Run the full spend/orchestrator suite to confirm the existing `buildSpend`-level tests (which don't go through `handleSpend`) are untouched**

Run: `go test ./ee/orchestrator/... -run TestBuildSpend -v`
Run: `go test ./ee/orchestrator/... -v`
Expected: PASS

- [ ] **Step 6: `gofmt -w`, commit**

```bash
gofmt -w ee/orchestrator/
git add ee/orchestrator/spend_api.go ee/orchestrator/spend_api_test.go
git commit -m "feat(orchestrator): add quota ceilings to the spend endpoint"
```

---

## Task 2: `GET /admin/users` cross-tenant search

**Files:**
- Modify: `ee/auth/admin.go` (register the route in `AdminRouter`, near line 34; add the handler)
- Test: `ee/auth/admin_users_search_test.go`

**Interfaces:**
- Consumes: `store.User` (existing, `pkg/store/models.go:42`), `store.Organization` (existing).
- Produces: `GET /admin/users?search=&limit=&offset=` per spec §3 Group E, response shape adjusted to what's actually queryable (no `auth_provider`/`last_active_at` per-user unless those columns already exist on `User` — check before including them; `User.OAuthProvider` exists per `models.go:48`, use that for `auth_provider`; there is no last-activity column on `User` itself — `DashboardSession` tracks activity per-user, per `handleListSessions` at `admin.go:608` — decide whether to join it here or omit `last_active_at`, and do not fabricate the field if you omit it).

- [ ] **Step 1: Write a failing test**

```go
package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestHandleAdminUsersSearch(t *testing.T) {
	db := newTestDB(t) // existing helper from admin_test.go
	org := Organization{ID: "org-1", Name: "Acme"}
	require.NoError(t, db.Create(&org).Error)
	user := User{ID: "usr-1", Email: "sarah@acme.example", Name: "Sarah Chen", OrgID: "org-1", Role: "admin"}
	require.NoError(t, db.Create(&user).Error)

	mux := http.NewServeMux()
	AdminRouter(db, mux)

	req := httptest.NewRequest(http.MethodGet, "/admin/users?search=sarah&limit=50&offset=0", nil).
		WithContext(ctxWithServerToken(t)) // existing helper pattern from admin_test.go's other /admin/* tests
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var resp struct {
		Users []struct {
			Email  string `json:"email"`
			OrgID  string `json:"org_id"`
			Role   string `json:"role"`
		} `json:"users"`
		Total int64 `json:"total"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Len(t, resp.Users, 1)
	require.Equal(t, "sarah@acme.example", resp.Users[0].Email)
	require.Equal(t, int64(1), resp.Total)
}

func TestHandleAdminUsersSearchRequiresSuperAdmin(t *testing.T) {
	db := newTestDB(t)
	mux := http.NewServeMux()
	AdminRouter(db, mux)

	req := httptest.NewRequest(http.MethodGet, "/admin/users", nil) // no auth
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	require.Equal(t, http.StatusForbidden, w.Code)
}
```

Check `admin_test.go` for the exact name of the "authenticate as super-admin" context helper (referenced above as `ctxWithServerToken` — this is a placeholder name; use whatever the existing tests in this file actually call, e.g. the pattern around `admin_test.go:143`'s `reqStats := ... .WithContext(ctx)`).

- [ ] **Step 2: Run, confirm both fail**

Run: `go test ./ee/auth/... -run TestHandleAdminUsersSearch -v`
Expected: FAIL — 404, route doesn't exist.

- [ ] **Step 3: Implement**

Register in `AdminRouter` (`admin.go`), alongside `/admin/stats` (line 34):

```go
	mux.HandleFunc("/admin/users", func(w http.ResponseWriter, r *http.Request) {
		if !isAdminAuthorized(r) {
			http.Error(w, "Forbidden: admin access required", http.StatusForbidden)
			return
		}
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		handleAdminUsersSearch(db, w, r)
	})
```

```go
// AdminUserSearchRow is one row of the cross-tenant user directory search.
// LastActiveAt is omitted, not fabricated: User carries no activity column of
// its own (DashboardSession does, per-user, but joining it here would make
// this a different, heavier query than "search users by name/email" — add it
// as a follow-up if the frontend needs it, with its own test).
type AdminUserSearchRow struct {
	ID      string `json:"id"`
	Email   string `json:"email"`
	Name    string `json:"name"`
	OrgID   string `json:"org_id"`
	OrgName string `json:"org_name"`
	Role    string `json:"role"`
	AuthProvider string `json:"auth_provider,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// handleAdminUsersSearch serves GET /admin/users?search=&limit=&offset=,
// searching by email or name substring across every org — the one directory
// view /admin/orgs/{orgID}/users (org-scoped) can't provide. Search is a
// simple ILIKE, matching the substring-search style already used elsewhere
// in this file (none currently exists to mirror exactly; this is genuinely
// new, so keep it simple rather than reaching for full-text search).
func handleAdminUsersSearch(db *gorm.DB, w http.ResponseWriter, r *http.Request) {
	limit := 50
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 200 {
			limit = n
		}
	}
	offset := 0
	if v := r.URL.Query().Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			offset = n
		}
	}
	search := strings.TrimSpace(r.URL.Query().Get("search"))

	q := db.Model(&User{})
	if search != "" {
		like := "%" + search + "%"
		q = q.Where("email ILIKE ? OR name ILIKE ?", like, like)
	}

	var total int64
	if err := q.Count(&total).Error; err != nil {
		http.Error(w, "Failed to count users", http.StatusInternalServerError)
		return
	}

	var users []User
	if err := q.Order("created_at desc").Limit(limit).Offset(offset).Find(&users).Error; err != nil {
		http.Error(w, "Failed to search users", http.StatusInternalServerError)
		return
	}

	orgIDs := make([]string, 0, len(users))
	seen := map[string]bool{}
	for _, u := range users {
		if !seen[u.OrgID] {
			seen[u.OrgID] = true
			orgIDs = append(orgIDs, u.OrgID)
		}
	}
	var orgs []Organization
	orgName := map[string]string{}
	if len(orgIDs) > 0 {
		db.Where("id IN ?", orgIDs).Find(&orgs)
		for _, o := range orgs {
			orgName[o.ID] = o.Name
		}
	}

	rows := make([]AdminUserSearchRow, len(users))
	for i, u := range users {
		provider := ""
		if u.OAuthProvider != nil {
			provider = *u.OAuthProvider
		}
		rows[i] = AdminUserSearchRow{
			ID: u.ID, Email: u.Email, Name: u.Name, OrgID: u.OrgID,
			OrgName: orgName[u.OrgID], Role: u.Role, AuthProvider: provider,
			CreatedAt: u.CreatedAt,
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"users": rows, "total": total})
}
```

Note: `ILIKE` is Postgres-specific. Check whether this codebase's test suite runs `ee/auth` tests against SQLite (like `pkg/store`'s tests do, per the earlier finding that `adminModelUsage`'s doc comment explicitly calls out a SQLite/Postgres split) — if so, `ILIKE` will fail there. Check `ee/auth/admin_test.go`'s `newTestDB` helper for which database it opens before assuming `ILIKE` works in tests; if it's SQLite, use `LOWER(email) LIKE LOWER(?)` instead (portable) rather than special-casing the query per-driver.

- [ ] **Step 4: Run the tests, confirm they pass**

Run: `go test ./ee/auth/... -run TestHandleAdminUsersSearch -v`
Expected: PASS

- [ ] **Step 5: Run the full `ee/auth` suite**

Run: `go test ./ee/auth/... -v`
Expected: PASS

- [ ] **Step 6: `gofmt -w`, commit**

```bash
gofmt -w ee/auth/
git add ee/auth/admin.go ee/auth/admin_users_search_test.go
git commit -m "feat(auth): add cross-tenant admin user search"
```

---

## Verification & Handoff Checklist

1. [ ] `gofmt -l cmd/ pkg/ ee/` returns 0 modified files.
2. [ ] `go test ./pkg/...` and `go test ./ee/...` pass 100%.
3. [ ] `go build ./...` succeeds.
4. [ ] `GET /api/v1/spend`'s existing response fields (`CostUSD`, `Allowance`, etc.) are byte-identical for a request that predates this plan — confirm by re-running whatever test suite already covers `buildSpend` unmodified.
5. [ ] `GET /admin/users` is unreachable without `isAdminAuthorized` — confirm `TestHandleAdminUsersSearchRequiresSuperAdmin` (or equivalent) actually exercises the *no-auth* path, not just a wrong-org path (there is no "wrong org" for a cross-tenant search — the only real check is super-admin vs. not).
