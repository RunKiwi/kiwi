# Org-Admin Self-Service Actions Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let an org-scoped admin (`User.Role == "admin"`) self-serve users/keys/audit-logs/usage/provider-config/join-requests/domain-join/rename for their own org, without touching the global super-admin-only lifecycle actions (create org, plan, grants, activate/suspend).

**Architecture:** A new `authorizeOrgAccess(r, orgID)` helper sits alongside the existing `isAdminAuthorized(r)` in `ee/auth/admin.go`. Nine route groups under `/admin/orgs/{orgID}/...` switch their gate from the former to the latter (including `model_usage`, an endpoint that landed on `main` after this plan was first drafted — see the drift notes on Tasks 2, 6, and 7); the four lifecycle routes keep the super-admin-only gate. One new endpoint (`PUT /admin/orgs/{orgID}/name`) is added for renaming, and `/auth/validate` gains two fields so the self-service frontend page can seed its UI without calling the super-admin-only "list all orgs" endpoint. On the frontend, the existing super-admin org-detail page's UI is extracted into a shared, prop-driven `OrgManagementPanel` component, reused by both the existing super-admin page and a new self-service `/team` page.

**Tech Stack:** Go 1.25 + GORM + SQLite (tests) / Postgres (prod) for the backend (`ee/auth`, BSL-licensed); Next.js (App Router) + TypeScript + Tailwind for the frontend (`frontend/`).

## Global Constraints

- `gofmt -l cmd/ pkg/ ee/` must print nothing after every Go change (fix: `gofmt -w cmd/ pkg/ ee/`).
- `CGO_ENABLED=0 go vet ./...` must be clean after every Go change.
- `CGO_ENABLED=0 go build ./...` must build after every Go change.
- `CGO_ENABLED=0 go test ./ee/auth/...` must pass after every Go change in this plan (the repo's mandated pre-commit suite runs `go test ./pkg/...` only, which doesn't cover `ee/`, but every task in this plan lives in `ee/auth` — run its tests directly).
- This repo's Next.js version has behavior different from training data (see `frontend/AGENTS.md`) — if an API you reach for doesn't behave as expected, check `frontend/node_modules/next/dist/docs/` rather than assuming a bug.
- `npm run build` (in `frontend/`) must succeed (full TypeScript type-check + compile) after every frontend change — there is no component test framework in this repo, so this is the closest thing to a test gate per task.
- No new endpoints beyond the one specified (`PUT /admin/orgs/{orgID}/name`) — everything else self-service maps onto an existing handler.
- Every mutating handler in `ee/auth/admin.go` logs via `LogAuditEvent(db, r, action, resource, resourceID, details)` — match that pattern for the new handler.

---

### Task 1: Harden API-key handlers against cross-org access

**Files:**
- Modify: `ee/auth/admin.go:112-140` (route switch: the two `users/{userID}/keys` cases)
- Modify: `ee/auth/admin.go:407-488` (`handleCreateAPIKey`, `handleListAPIKeys`, `handleRevokeAPIKey`)
- Test: `ee/auth/admin_test.go`

**Interfaces:**
- Produces: `handleCreateAPIKey(db *gorm.DB, w http.ResponseWriter, r *http.Request, orgID, userID string)`, `handleListAPIKeys(db *gorm.DB, w http.ResponseWriter, r *http.Request, orgID, userID string)`, `handleRevokeAPIKey(db *gorm.DB, w http.ResponseWriter, r *http.Request, orgID, keyID string)` — all three now take `orgID` as their first string argument. Task 2 and Task 3 build on these signatures.

Today these three handlers take the target `userID`/`keyID` straight from the URL and never check it belongs to the `orgID` earlier in the same path — harmless while only a super-admin (who can touch any org already) can reach these routes, but a real cross-org escalation once org-scoped admins are let in by Task 2. Fix this first, before any org-scoped admin can reach these routes at all.

- [ ] **Step 1: Write the failing test**

Add this test to `ee/auth/admin_test.go` (existing imports — `bytes`, `context`, `encoding/json`, `fmt`, `net/http`, `net/http/httptest`, `os`, `testing` — already cover everything it needs):

```go
func TestAPIKeyHandlers_RejectCrossOrgUser(t *testing.T) {
	db := setupTestDB(t)
	mux := http.NewServeMux()
	AdminRouter(db, mux)

	orgA := Organization{ID: "org-a", Name: "Org A"}
	orgB := Organization{ID: "org-b", Name: "Org B"}
	db.Create(&orgA)
	db.Create(&orgB)
	userInB := User{ID: "user-b", Email: "b@example.com", Name: "User B", OrgID: "org-b", Role: "member"}
	db.Create(&userInB)

	claims := &UserClaims{UserID: "system"}
	ctx := ContextWithClaims(context.Background(), claims)

	// Create a key for a user in org B via org A's path — must be rejected.
	reqCreate := httptest.NewRequest(http.MethodPost, "/admin/orgs/org-a/users/user-b/keys", bytes.NewReader([]byte(`{"label":"test"}`))).WithContext(ctx)
	wCreate := httptest.NewRecorder()
	mux.ServeHTTP(wCreate, reqCreate)
	if wCreate.Code != http.StatusNotFound {
		t.Errorf("expected 404 creating key for cross-org user, got %d: %s", wCreate.Code, wCreate.Body.String())
	}

	// List keys for a user in org B via org A's path — must be rejected.
	reqList := httptest.NewRequest(http.MethodGet, "/admin/orgs/org-a/users/user-b/keys", nil).WithContext(ctx)
	wList := httptest.NewRecorder()
	mux.ServeHTTP(wList, reqList)
	if wList.Code != http.StatusNotFound {
		t.Errorf("expected 404 listing keys for cross-org user, got %d: %s", wList.Code, wList.Body.String())
	}

	// Revoke a key belonging to a user in org B via org A's path — must be rejected.
	_, key, err := GenerateAPIKey(userInB.ID, "b-key", nil)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	if err := db.Create(key).Error; err != nil {
		t.Fatalf("save key: %v", err)
	}
	reqRevoke := httptest.NewRequest(http.MethodDelete, "/admin/orgs/org-a/users/user-b/keys/"+key.ID, nil).WithContext(ctx)
	wRevoke := httptest.NewRecorder()
	mux.ServeHTTP(wRevoke, reqRevoke)
	if wRevoke.Code != http.StatusNotFound {
		t.Errorf("expected 404 revoking cross-org key, got %d: %s", wRevoke.Code, wRevoke.Body.String())
	}

	// The key must still be active — the rejected revoke must not have taken effect.
	var stillActive APIKey
	if err := db.First(&stillActive, "id = ?", key.ID).Error; err != nil {
		t.Fatalf("key vanished: %v", err)
	}
	if stillActive.RevokedAt != nil {
		t.Errorf("cross-org revoke must not have taken effect, but revoked_at is set")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `CGO_ENABLED=0 go test ./ee/auth/... -run TestAPIKeyHandlers_RejectCrossOrgUser -v`
Expected: build failure — `handleCreateAPIKey`/`handleListAPIKeys`/`handleRevokeAPIKey` don't yet accept an `orgID` argument, and the route switch doesn't yet pass one. (This test can't compile until Step 3's signature change and Step 4's call-site update both land — that's expected; do them together, then come back to this step to confirm the test itself now compiles and fails on assertions, not compilation, before Step 5 fixes it. If your test harness insists on green-red-green, you may skip straight to writing Steps 3–5 together and run once at Step 6.)

- [ ] **Step 3: Change the three handler signatures and add the org check**

In `ee/auth/admin.go`, replace:

```go
func handleCreateAPIKey(db *gorm.DB, w http.ResponseWriter, r *http.Request, userID string) {
	// Verify user exists.
	var user User
	if err := db.First(&user, "id = ?", userID).Error; err != nil {
		http.Error(w, "User not found", http.StatusNotFound)
		return
	}
```

with:

```go
func handleCreateAPIKey(db *gorm.DB, w http.ResponseWriter, r *http.Request, orgID, userID string) {
	// Verify user exists and belongs to orgID — without this, an org-scoped
	// admin (authorized only for their own orgID) could mint a key for a
	// user in a different org just by supplying that user's ID.
	var user User
	if err := db.First(&user, "id = ?", userID).Error; err != nil || user.OrgID != orgID {
		http.Error(w, "User not found", http.StatusNotFound)
		return
	}
```

Replace:

```go
func handleListAPIKeys(db *gorm.DB, w http.ResponseWriter, r *http.Request, userID string) {
	var keys []APIKey
	if err := db.Where("user_id = ? AND revoked_at IS NULL", userID).Order("created_at desc").Find(&keys).Error; err != nil {
		http.Error(w, "Failed to list keys", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(keys)
}
```

with:

```go
func handleListAPIKeys(db *gorm.DB, w http.ResponseWriter, r *http.Request, orgID, userID string) {
	var user User
	if err := db.First(&user, "id = ?", userID).Error; err != nil || user.OrgID != orgID {
		http.Error(w, "User not found", http.StatusNotFound)
		return
	}
	var keys []APIKey
	if err := db.Where("user_id = ? AND revoked_at IS NULL", userID).Order("created_at desc").Find(&keys).Error; err != nil {
		http.Error(w, "Failed to list keys", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(keys)
}
```

Replace:

```go
func handleRevokeAPIKey(db *gorm.DB, w http.ResponseWriter, r *http.Request, keyID string) {
	now := time.Now()
	result := db.Model(&APIKey{}).Where("id = ? AND revoked_at IS NULL", keyID).Update("revoked_at", &now)
	if result.Error != nil {
		http.Error(w, "Failed to revoke key", http.StatusInternalServerError)
		return
	}
	if result.RowsAffected == 0 {
		http.Error(w, "Key not found or already revoked", http.StatusNotFound)
		return
	}

	_ = LogAuditEvent(db, r, "REVOKE", "API_KEY", keyID, "Revoked API Key")

	w.WriteHeader(http.StatusNoContent)
}
```

with:

```go
func handleRevokeAPIKey(db *gorm.DB, w http.ResponseWriter, r *http.Request, orgID, keyID string) {
	var key APIKey
	if err := db.First(&key, "id = ?", keyID).Error; err != nil {
		http.Error(w, "Key not found or already revoked", http.StatusNotFound)
		return
	}
	var user User
	if err := db.First(&user, "id = ?", key.UserID).Error; err != nil || user.OrgID != orgID {
		http.Error(w, "Key not found or already revoked", http.StatusNotFound)
		return
	}

	now := time.Now()
	result := db.Model(&APIKey{}).Where("id = ? AND revoked_at IS NULL", keyID).Update("revoked_at", &now)
	if result.Error != nil {
		http.Error(w, "Failed to revoke key", http.StatusInternalServerError)
		return
	}
	if result.RowsAffected == 0 {
		http.Error(w, "Key not found or already revoked", http.StatusNotFound)
		return
	}

	_ = LogAuditEvent(db, r, "REVOKE", "API_KEY", keyID, "Revoked API Key")

	w.WriteHeader(http.StatusNoContent)
}
```

- [ ] **Step 4: Update the two call sites to pass `orgID`**

In `ee/auth/admin.go`, replace:

```go
		case len(parts) == 4 && parts[1] == "users" && parts[3] == "keys":
			userID := parts[2]
			switch r.Method {
			case http.MethodPost:
				handleCreateAPIKey(db, w, r, userID)
			case http.MethodGet:
				handleListAPIKeys(db, w, r, userID)
			default:
				http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			}
```

with:

```go
		case len(parts) == 4 && parts[1] == "users" && parts[3] == "keys":
			orgID := parts[0]
			userID := parts[2]
			switch r.Method {
			case http.MethodPost:
				handleCreateAPIKey(db, w, r, orgID, userID)
			case http.MethodGet:
				handleListAPIKeys(db, w, r, orgID, userID)
			default:
				http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			}
```

Replace:

```go
		case len(parts) == 5 && parts[1] == "users" && parts[3] == "keys":
			keyID := parts[4]
			if r.Method == http.MethodDelete {
				handleRevokeAPIKey(db, w, r, keyID)
			} else {
				http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			}
```

with:

```go
		case len(parts) == 5 && parts[1] == "users" && parts[3] == "keys":
			orgID := parts[0]
			keyID := parts[4]
			if r.Method == http.MethodDelete {
				handleRevokeAPIKey(db, w, r, orgID, keyID)
			} else {
				http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `CGO_ENABLED=0 go test ./ee/auth/... -run TestAPIKeyHandlers_RejectCrossOrgUser -v`
Expected: PASS

- [ ] **Step 6: Run the full package test suite to check for regressions**

Run: `CGO_ENABLED=0 go test ./ee/auth/... -v`
Expected: all PASS (in particular `TestAdminAPIEndpoints` and `TestAdminRouter_ClaimsAuth`, which exercise other routes in the same switch and must be unaffected)

- [ ] **Step 7: gofmt, vet, build**

Run: `gofmt -w cmd/ pkg/ ee/ && CGO_ENABLED=0 go vet ./... && CGO_ENABLED=0 go build ./...`
Expected: clean (no gofmt diff, no vet errors, build succeeds)

- [ ] **Step 8: Commit**

```bash
git add ee/auth/admin.go ee/auth/admin_test.go
git commit -m "$(cat <<'EOF'
fix(auth): reject cross-org userID/keyID in API key handlers

handleCreateAPIKey, handleListAPIKeys, and handleRevokeAPIKey took
the target userID/keyID straight from the URL without checking it
belongs to the orgID earlier in the same path. Harmless today since
only a super-admin (who can already touch any org) can reach these
routes — but a cross-org escalation once org-scoped admins are let
into their own org's routes. Fixing this first, ahead of that change.
EOF
)"
```

---

### Task 2: Add `authorizeOrgAccess` and relax the gate on nine route groups

**Drift note (found during SDD setup, 2026-08-09):** `main` gained a
`GET /admin/orgs/{orgID}/model_usage` route (and its consuming "Usage" tab
on the frontend) after this plan was written. It's included below as a
ninth self-service route group, gated the same way as `usage`. Everything
else in this task is unchanged from the original plan.

**Files:**
- Modify: `ee/auth/admin.go:27-195` (`AdminRouter`'s `/admin/orgs/` handler and route switch)
- Test: `ee/auth/admin_test.go`

**Interfaces:**
- Consumes: `handleCreateAPIKey(db, w, r, orgID, userID string)`, `handleListAPIKeys(db, w, r, orgID, userID string)`, `handleRevokeAPIKey(db, w, r, orgID, keyID string)` from Task 1.
- Produces: `authorizeOrgAccess(r *http.Request, orgID string) bool` — Task 3 uses this for the new rename route.

Today `isAdminAuthorized(r)` is checked once, at the very top of the `/admin/orgs/` handler, before the path is even parsed. This task moves that parsing above the check and pushes the authorization decision into each `case`, so nine of the thirteen route groups can use a new, more permissive check while the other four keep the original one.

- [ ] **Step 1: Write the failing test**

Add to `ee/auth/admin_test.go`. This test needs `ee/audit` (for `audit.AuditLog`, which `setupTestDB`'s `InitAuthDB` does not migrate — see `ee/auth/audit_helper_test.go` for the same pattern) — add `"github.com/ibreakthecloud/kiwi/ee/audit"` to the import block first.

```go
func TestAdminRouter_OrgScopedSelfService(t *testing.T) {
	db := setupTestDB(t)
	if err := db.AutoMigrate(&audit.AuditLog{}); err != nil {
		t.Fatalf("migrate audit table: %v", err)
	}
	mux := http.NewServeMux()
	AdminRouter(db, mux)

	orgA := Organization{ID: "org-a", Name: "Org A", Plan: "free"}
	orgB := Organization{ID: "org-b", Name: "Org B", Plan: "free"}
	db.Create(&orgA)
	db.Create(&orgB)
	userA := User{ID: "user-a-1", Email: "a1@example.com", Name: "User A1", OrgID: "org-a", Role: "member"}
	db.Create(&userA)

	adminA := &UserClaims{UserID: "admin-a", OrgID: "org-a", Role: "admin"}
	adminB := &UserClaims{UserID: "admin-b", OrgID: "org-b", Role: "admin"}
	memberA := &UserClaims{UserID: "member-a", OrgID: "org-a", Role: "member"}

	type routeCase struct {
		name          string
		method        string
		path          string
		body          string
		wantOwnStatus int // -1 means "anything but 403" — used where the handler's
		                  // own internal behavior is out of scope for this test
	}

	cases := []routeCase{
		{"users list", http.MethodGet, "/admin/orgs/org-a/users", "", http.StatusOK},
		{"keys list", http.MethodGet, "/admin/orgs/org-a/users/user-a-1/keys", "", http.StatusOK},
		{"audit", http.MethodGet, "/admin/orgs/org-a/audit", "", http.StatusOK},
		{"usage", http.MethodGet, "/admin/orgs/org-a/usage", "", -1},
		{"model_usage", http.MethodGet, "/admin/orgs/org-a/model_usage", "", -1},
		{"provider get", http.MethodGet, "/admin/orgs/org-a/provider", "", http.StatusOK},
		{"join_requests list", http.MethodGet, "/admin/orgs/org-a/join_requests", "", http.StatusOK},
		{"domain_join", http.MethodPut, "/admin/orgs/org-a/domain_join", `{"domain_join":true}`, http.StatusOK},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reqOwn := httptest.NewRequest(tc.method, tc.path, bytes.NewReader([]byte(tc.body)))
			reqOwn = reqOwn.WithContext(ContextWithClaims(reqOwn.Context(), adminA))
			wOwn := httptest.NewRecorder()
			mux.ServeHTTP(wOwn, reqOwn)
			if wOwn.Code == http.StatusForbidden {
				t.Errorf("org-admin should access own org's %s, got 403: %s", tc.name, wOwn.Body.String())
			}
			if tc.wantOwnStatus != -1 && wOwn.Code != tc.wantOwnStatus {
				t.Errorf("expected %d for own-org %s, got %d: %s", tc.wantOwnStatus, tc.name, wOwn.Code, wOwn.Body.String())
			}

			reqCross := httptest.NewRequest(tc.method, tc.path, bytes.NewReader([]byte(tc.body)))
			reqCross = reqCross.WithContext(ContextWithClaims(reqCross.Context(), adminB))
			wCross := httptest.NewRecorder()
			mux.ServeHTTP(wCross, reqCross)
			if wCross.Code != http.StatusForbidden {
				t.Errorf("cross-org admin should be rejected on %s, got %d", tc.name, wCross.Code)
			}

			reqMember := httptest.NewRequest(tc.method, tc.path, bytes.NewReader([]byte(tc.body)))
			reqMember = reqMember.WithContext(ContextWithClaims(reqMember.Context(), memberA))
			wMember := httptest.NewRecorder()
			mux.ServeHTTP(wMember, reqMember)
			if wMember.Code != http.StatusForbidden {
				t.Errorf("member should be rejected on %s, got %d", tc.name, wMember.Code)
			}
		})
	}

	// Super-admin regression: still works on any org via the bootstrap token.
	t.Setenv("KIWI_SERVER_TOKEN", "super-secret")
	reqSuper := httptest.NewRequest(http.MethodGet, "/admin/orgs/org-b/users", nil)
	reqSuper.Header.Set("Authorization", "Bearer super-secret")
	wSuper := httptest.NewRecorder()
	mux.ServeHTTP(wSuper, reqSuper)
	if wSuper.Code != http.StatusOK {
		t.Errorf("super-admin should still access any org, got %d", wSuper.Code)
	}

	// Lifecycle actions stay super-admin-only even for an org's own admin.
	reqPlan := httptest.NewRequest(http.MethodPost, "/admin/orgs/org-a/plan", bytes.NewReader([]byte(`{"plan":"pro"}`)))
	reqPlan = reqPlan.WithContext(ContextWithClaims(reqPlan.Context(), adminA))
	wPlan := httptest.NewRecorder()
	mux.ServeHTTP(wPlan, reqPlan)
	if wPlan.Code != http.StatusForbidden {
		t.Errorf("org-admin must not be able to change their own org's plan, got %d", wPlan.Code)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `CGO_ENABLED=0 go test ./ee/auth/... -run TestAdminRouter_OrgScopedSelfService -v`
Expected: FAIL — every sub-test's "own org" request currently gets 403, because `isAdminAuthorized` still gates the whole tree and `adminA`/`adminB`/`memberA` never pass it.

- [ ] **Step 3: Add the `authorizeOrgAccess` helper**

In `ee/auth/admin.go`, immediately after the closing brace of `isAdminAuthorized` (the function ending just before `func handleCreateOrg`), add:

```go
// authorizeOrgAccess grants access to super-admins (via isAdminAuthorized) or
// to an org-scoped admin acting on their own org.
func authorizeOrgAccess(r *http.Request, orgID string) bool {
	if isAdminAuthorized(r) {
		return true
	}
	claims := ClaimsFromContext(r.Context())
	return claims != nil && claims.IsAdmin() && claims.OrgID == orgID
}
```

- [ ] **Step 4: Move the blanket guard out and parse the path first**

Replace:

```go
	mux.HandleFunc("/admin/orgs/", func(w http.ResponseWriter, r *http.Request) {
		if !isAdminAuthorized(r) {
			http.Error(w, "Forbidden: admin access required", http.StatusForbidden)
			return
		}

		// /admin/orgs/{orgID}/users[/{userID}/keys[/{keyID}]]
		path := strings.TrimPrefix(r.URL.Path, "/admin/orgs/")
		parts := strings.Split(path, "/")

		switch {
		case len(parts) == 2 && parts[1] == "activate":
			orgID := parts[0]
```

with:

```go
	mux.HandleFunc("/admin/orgs/", func(w http.ResponseWriter, r *http.Request) {
		// /admin/orgs/{orgID}/users[/{userID}/keys[/{keyID}]]
		path := strings.TrimPrefix(r.URL.Path, "/admin/orgs/")
		parts := strings.Split(path, "/")

		switch {
		case len(parts) == 2 && parts[1] == "activate":
			if !isAdminAuthorized(r) {
				http.Error(w, "Forbidden: admin access required", http.StatusForbidden)
				return
			}
			orgID := parts[0]
```

- [ ] **Step 5: Add the guard to the four routes that stay super-admin-only**

Replace:

```go
		case len(parts) == 2 && parts[1] == "suspend":
			orgID := parts[0]
```

with:

```go
		case len(parts) == 2 && parts[1] == "suspend":
			if !isAdminAuthorized(r) {
				http.Error(w, "Forbidden: admin access required", http.StatusForbidden)
				return
			}
			orgID := parts[0]
```

Replace:

```go
		case len(parts) == 2 && parts[1] == "plan":
			orgID := parts[0]
```

with:

```go
		case len(parts) == 2 && parts[1] == "plan":
			if !isAdminAuthorized(r) {
				http.Error(w, "Forbidden: admin access required", http.StatusForbidden)
				return
			}
			orgID := parts[0]
```

Replace:

```go
		case len(parts) == 2 && parts[1] == "grant":
			orgID := parts[0]
```

with:

```go
		case len(parts) == 2 && parts[1] == "grant":
			if !isAdminAuthorized(r) {
				http.Error(w, "Forbidden: admin access required", http.StatusForbidden)
				return
			}
			orgID := parts[0]
```

(The fourth super-admin-only route, top-level create/list at `/admin/orgs` — not `/admin/orgs/`, a separate `mux.HandleFunc` registration — already has its own unchanged `isAdminAuthorized` check and needs no edit.)

- [ ] **Step 6: Add `authorizeOrgAccess` to the nine self-service routes**

Replace:

```go
		case len(parts) == 2 && parts[1] == "usage":
			orgID := parts[0]
			if r.Method != http.MethodGet {
```

with:

```go
		case len(parts) == 2 && parts[1] == "usage":
			orgID := parts[0]
			if !authorizeOrgAccess(r, orgID) {
				http.Error(w, "Forbidden: admin access required", http.StatusForbidden)
				return
			}
			if r.Method != http.MethodGet {
```

Replace (this route did not exist when the plan was first drafted — it was added by a PR that landed on `main` during planning; verify the exact surrounding text against the current file before applying, since `handleOrgModelUsageAdmin`'s body is not otherwise touched by this task):

```go
		case len(parts) == 2 && parts[1] == "model_usage":
			orgID := parts[0]
			if r.Method != http.MethodGet {
```

with:

```go
		case len(parts) == 2 && parts[1] == "model_usage":
			orgID := parts[0]
			if !authorizeOrgAccess(r, orgID) {
				http.Error(w, "Forbidden: admin access required", http.StatusForbidden)
				return
			}
			if r.Method != http.MethodGet {
```

Replace:

```go
		case len(parts) == 2 && parts[1] == "audit":
			orgID := parts[0]
			if r.Method != http.MethodGet {
```

with:

```go
		case len(parts) == 2 && parts[1] == "audit":
			orgID := parts[0]
			if !authorizeOrgAccess(r, orgID) {
				http.Error(w, "Forbidden: admin access required", http.StatusForbidden)
				return
			}
			if r.Method != http.MethodGet {
```

Replace:

```go
		case len(parts) == 2 && parts[1] == "provider":
			orgID := parts[0]
			switch r.Method {
```

with:

```go
		case len(parts) == 2 && parts[1] == "provider":
			orgID := parts[0]
			if !authorizeOrgAccess(r, orgID) {
				http.Error(w, "Forbidden: admin access required", http.StatusForbidden)
				return
			}
			switch r.Method {
```

Replace:

```go
		case len(parts) == 2 && parts[1] == "users":
			orgID := parts[0]
			switch r.Method {
			case http.MethodPost:
				handleCreateUser(db, w, r, orgID)
			case http.MethodGet:
				handleListUsers(db, w, r, orgID)
```

with:

```go
		case len(parts) == 2 && parts[1] == "users":
			orgID := parts[0]
			if !authorizeOrgAccess(r, orgID) {
				http.Error(w, "Forbidden: admin access required", http.StatusForbidden)
				return
			}
			switch r.Method {
			case http.MethodPost:
				handleCreateUser(db, w, r, orgID)
			case http.MethodGet:
				handleListUsers(db, w, r, orgID)
```

Replace (this is the state Task 1 left it in):

```go
		case len(parts) == 4 && parts[1] == "users" && parts[3] == "keys":
			orgID := parts[0]
			userID := parts[2]
			switch r.Method {
```

with:

```go
		case len(parts) == 4 && parts[1] == "users" && parts[3] == "keys":
			orgID := parts[0]
			userID := parts[2]
			if !authorizeOrgAccess(r, orgID) {
				http.Error(w, "Forbidden: admin access required", http.StatusForbidden)
				return
			}
			switch r.Method {
```

Replace (this is the state Task 1 left it in):

```go
		case len(parts) == 5 && parts[1] == "users" && parts[3] == "keys":
			orgID := parts[0]
			keyID := parts[4]
			if r.Method == http.MethodDelete {
```

with:

```go
		case len(parts) == 5 && parts[1] == "users" && parts[3] == "keys":
			orgID := parts[0]
			keyID := parts[4]
			if !authorizeOrgAccess(r, orgID) {
				http.Error(w, "Forbidden: admin access required", http.StatusForbidden)
				return
			}
			if r.Method == http.MethodDelete {
```

Replace:

```go
		case len(parts) == 2 && parts[1] == "join_requests":
			orgID := parts[0]
			if r.Method == http.MethodGet {
```

with:

```go
		case len(parts) == 2 && parts[1] == "join_requests":
			orgID := parts[0]
			if !authorizeOrgAccess(r, orgID) {
				http.Error(w, "Forbidden: admin access required", http.StatusForbidden)
				return
			}
			if r.Method == http.MethodGet {
```

Replace:

```go
		case len(parts) == 4 && parts[1] == "join_requests" && parts[3] == "approve":
			orgID := parts[0]
			reqID := parts[2]
			if r.Method == http.MethodPost {
```

with:

```go
		case len(parts) == 4 && parts[1] == "join_requests" && parts[3] == "approve":
			orgID := parts[0]
			reqID := parts[2]
			if !authorizeOrgAccess(r, orgID) {
				http.Error(w, "Forbidden: admin access required", http.StatusForbidden)
				return
			}
			if r.Method == http.MethodPost {
```

Replace:

```go
		case len(parts) == 4 && parts[1] == "join_requests" && parts[3] == "deny":
			orgID := parts[0]
			reqID := parts[2]
			if r.Method == http.MethodPost {
```

with:

```go
		case len(parts) == 4 && parts[1] == "join_requests" && parts[3] == "deny":
			orgID := parts[0]
			reqID := parts[2]
			if !authorizeOrgAccess(r, orgID) {
				http.Error(w, "Forbidden: admin access required", http.StatusForbidden)
				return
			}
			if r.Method == http.MethodPost {
```

Replace:

```go
		case len(parts) == 2 && parts[1] == "domain_join":
			orgID := parts[0]
			if r.Method == http.MethodPut {
```

with:

```go
		case len(parts) == 2 && parts[1] == "domain_join":
			orgID := parts[0]
			if !authorizeOrgAccess(r, orgID) {
				http.Error(w, "Forbidden: admin access required", http.StatusForbidden)
				return
			}
			if r.Method == http.MethodPut {
```

- [ ] **Step 7: Run test to verify it passes**

Run: `CGO_ENABLED=0 go test ./ee/auth/... -run TestAdminRouter_OrgScopedSelfService -v`
Expected: PASS

- [ ] **Step 8: Run the full package test suite to check for regressions**

Run: `CGO_ENABLED=0 go test ./ee/auth/... -v`
Expected: all PASS

- [ ] **Step 9: gofmt, vet, build**

Run: `gofmt -w cmd/ pkg/ ee/ && CGO_ENABLED=0 go vet ./... && CGO_ENABLED=0 go build ./...`
Expected: clean

- [ ] **Step 10: Commit**

```bash
git add ee/auth/admin.go ee/auth/admin_test.go
git commit -m "$(cat <<'EOF'
feat(auth): let org-scoped admins self-serve their own org

Adds authorizeOrgAccess alongside the existing super-admin-only
isAdminAuthorized, and switches the gate on nine route groups
(users, keys, audit, usage, model_usage, provider, join_requests,
domain_join) so an org's own admin can act on their own org —
closing the gap where join requests and domain-join approval
required a Kiwi operator, and making the Usage tab work for them too.
Lifecycle routes (create org, activate/suspend, plan, grant) are
untouched and stay super-admin-only.
EOF
)"
```

---

### Task 3: Add the organization-rename endpoint

**Files:**
- Modify: `ee/auth/admin.go` (route switch — new case; new handler function at end of file)
- Test: `ee/auth/admin_test.go`

**Interfaces:**
- Consumes: `authorizeOrgAccess(r, orgID)` from Task 2.
- Produces: `handleUpdateOrgName(db *gorm.DB, w http.ResponseWriter, r *http.Request, orgID string)`. Nothing downstream in this plan depends on it — the frontend calls it by URL (`PUT /admin/orgs/{orgID}/name`), not by Go symbol.

- [ ] **Step 1: Write the failing test**

Add to `ee/auth/admin_test.go`:

```go
func TestUpdateOrgName(t *testing.T) {
	db := setupTestDB(t)
	mux := http.NewServeMux()
	AdminRouter(db, mux)

	org := Organization{ID: "org-rename", Name: "Old Name"}
	db.Create(&org)
	other := Organization{ID: "org-other", Name: "Taken Name"}
	db.Create(&other)

	claims := &UserClaims{UserID: "system"}
	ctx := ContextWithClaims(context.Background(), claims)

	// Success.
	req := httptest.NewRequest(http.MethodPut, "/admin/orgs/org-rename/name", bytes.NewReader([]byte(`{"name":"New Name"}`))).WithContext(ctx)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var updated Organization
	db.First(&updated, "id = ?", "org-rename")
	if updated.Name != "New Name" {
		t.Errorf("expected renamed org, got %q", updated.Name)
	}

	// Empty name rejected.
	reqEmpty := httptest.NewRequest(http.MethodPut, "/admin/orgs/org-rename/name", bytes.NewReader([]byte(`{"name":"   "}`))).WithContext(ctx)
	wEmpty := httptest.NewRecorder()
	mux.ServeHTTP(wEmpty, reqEmpty)
	if wEmpty.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for empty name, got %d", wEmpty.Code)
	}

	// Duplicate name rejected.
	reqDup := httptest.NewRequest(http.MethodPut, "/admin/orgs/org-rename/name", bytes.NewReader([]byte(`{"name":"Taken Name"}`))).WithContext(ctx)
	wDup := httptest.NewRecorder()
	mux.ServeHTTP(wDup, reqDup)
	if wDup.Code != http.StatusConflict {
		t.Errorf("expected 409 for duplicate name, got %d", wDup.Code)
	}

	// Org-admin can rename their own org.
	adminClaims := &UserClaims{UserID: "admin-1", OrgID: "org-rename", Role: "admin"}
	reqSelf := httptest.NewRequest(http.MethodPut, "/admin/orgs/org-rename/name", bytes.NewReader([]byte(`{"name":"Self Renamed"}`))).WithContext(ContextWithClaims(context.Background(), adminClaims))
	wSelf := httptest.NewRecorder()
	mux.ServeHTTP(wSelf, reqSelf)
	if wSelf.Code != http.StatusOK {
		t.Errorf("expected org-admin to rename own org, got %d: %s", wSelf.Code, wSelf.Body.String())
	}

	// Org-admin cannot rename a different org.
	reqOther := httptest.NewRequest(http.MethodPut, "/admin/orgs/org-other/name", bytes.NewReader([]byte(`{"name":"Hijacked"}`))).WithContext(ContextWithClaims(context.Background(), adminClaims))
	wOther := httptest.NewRecorder()
	mux.ServeHTTP(wOther, reqOther)
	if wOther.Code != http.StatusForbidden {
		t.Errorf("expected 403 renaming a different org, got %d", wOther.Code)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `CGO_ENABLED=0 go test ./ee/auth/... -run TestUpdateOrgName -v`
Expected: FAIL — route returns 404 ("Not found", the switch's `default` case), since neither the route nor the handler exist yet.

- [ ] **Step 3: Add the handler**

At the end of `ee/auth/admin.go` (after `handleGrantOrgMinutes`), add:

```go
func handleUpdateOrgName(db *gorm.DB, w http.ResponseWriter, r *http.Request, orgID string) {
	var body struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "Bad request", http.StatusBadRequest)
		return
	}
	name := strings.TrimSpace(body.Name)
	if name == "" {
		http.Error(w, "Bad request: 'name' is required", http.StatusBadRequest)
		return
	}

	var org Organization
	if err := db.First(&org, "id = ?", orgID).Error; err != nil {
		http.Error(w, "Organization not found", http.StatusNotFound)
		return
	}

	org.Name = name
	if err := db.Save(&org).Error; err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			http.Error(w, "Organization name already exists", http.StatusConflict)
			return
		}
		http.Error(w, "Failed to rename organization", http.StatusInternalServerError)
		return
	}

	_ = LogAuditEvent(db, r, "UPDATE", "ORG_NAME", orgID, fmt.Sprintf("Renamed organization to %q", org.Name))

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(org)
}
```

- [ ] **Step 4: Wire the route**

In `ee/auth/admin.go`, replace:

```go
		case len(parts) == 2 && parts[1] == "domain_join":
			orgID := parts[0]
			if !authorizeOrgAccess(r, orgID) {
				http.Error(w, "Forbidden: admin access required", http.StatusForbidden)
				return
			}
			if r.Method == http.MethodPut {
				handleToggleDomainJoin(db, w, r, orgID)
			} else {
				http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			}

		default:
```

with:

```go
		case len(parts) == 2 && parts[1] == "domain_join":
			orgID := parts[0]
			if !authorizeOrgAccess(r, orgID) {
				http.Error(w, "Forbidden: admin access required", http.StatusForbidden)
				return
			}
			if r.Method == http.MethodPut {
				handleToggleDomainJoin(db, w, r, orgID)
			} else {
				http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			}

		case len(parts) == 2 && parts[1] == "name":
			orgID := parts[0]
			if !authorizeOrgAccess(r, orgID) {
				http.Error(w, "Forbidden: admin access required", http.StatusForbidden)
				return
			}
			if r.Method == http.MethodPut {
				handleUpdateOrgName(db, w, r, orgID)
			} else {
				http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			}

		default:
```

- [ ] **Step 5: Run test to verify it passes**

Run: `CGO_ENABLED=0 go test ./ee/auth/... -run TestUpdateOrgName -v`
Expected: PASS

- [ ] **Step 6: Run the full package test suite, gofmt, vet, build**

Run: `CGO_ENABLED=0 go test ./ee/auth/... -v && gofmt -w cmd/ pkg/ ee/ && CGO_ENABLED=0 go vet ./... && CGO_ENABLED=0 go build ./...`
Expected: all PASS, clean

- [ ] **Step 7: Commit**

```bash
git add ee/auth/admin.go ee/auth/admin_test.go
git commit -m "$(cat <<'EOF'
feat(auth): add PUT /admin/orgs/{orgID}/name to rename an org

No endpoint renamed an org before this. Follows the domain_join
route's single-field-replace convention and is gated the same as
the rest of the self-service batch — renaming isn't a billing or
lifecycle decision, so there's no reason to reserve it for
super-admins.
EOF
)"
```

---

### Task 4: Add `domain_join`/`primary_domain` to `/auth/validate`

**Files:**
- Modify: `ee/auth/admin.go:199-232` (the `/auth/validate` handler)
- Test: `ee/auth/admin_test.go`

**Interfaces:**
- Produces: `/auth/validate`'s JSON response gains two fields, `domain_join: bool` and `primary_domain: string`. Task 8 (frontend `/team` page) relies on these being present to seed the self-service `OrgManagementPanel` without calling the super-admin-only `listAdminOrgs`.

- [ ] **Step 1: Write the failing test**

Add to `ee/auth/admin_test.go`:

```go
func TestAuthValidate_IncludesDomainJoinFields(t *testing.T) {
	db := setupTestDB(t)
	mux := http.NewServeMux()
	AdminRouter(db, mux)

	org := Organization{ID: "org-validate", Name: "Validate Org", DomainJoin: true, PrimaryDomain: "example.com"}
	db.Create(&org)

	claims := &UserClaims{UserID: "user-1", OrgID: "org-validate", Role: "admin"}
	req := httptest.NewRequest(http.MethodGet, "/auth/validate", nil).WithContext(ContextWithClaims(context.Background(), claims))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		DomainJoin    bool   `json:"domain_join"`
		PrimaryDomain string `json:"primary_domain"`
		Role          string `json:"role"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !resp.DomainJoin || resp.PrimaryDomain != "example.com" || resp.Role != "admin" {
		t.Errorf("unexpected validate response: %+v", resp)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `CGO_ENABLED=0 go test ./ee/auth/... -run TestAuthValidate_IncludesDomainJoinFields -v`
Expected: FAIL — `resp.DomainJoin` decodes to `false` (the field isn't in the response yet) and `resp.PrimaryDomain` decodes to `""`.

- [ ] **Step 3: Add the fields**

In `ee/auth/admin.go`, replace:

```go
		// Look up org name for display.
		orgName := claims.OrgID
		activationState := "inactive"
		plan := "free"
		var org Organization
		if err := db.First(&org, "id = ?", claims.OrgID).Error; err == nil {
			orgName = org.Name
			activationState = org.ActivationState
			plan = org.Plan
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"user_id":          claims.UserID,
			"email":            claims.Email,
			"name":             claims.Name,
			"org_id":           claims.OrgID,
			"org_name":         orgName,
			"role":             claims.Role,
			"activation_state": activationState,
			"plan":             plan,
		})
```

with:

```go
		// Look up org name for display.
		orgName := claims.OrgID
		activationState := "inactive"
		plan := "free"
		domainJoin := false
		primaryDomain := ""
		var org Organization
		if err := db.First(&org, "id = ?", claims.OrgID).Error; err == nil {
			orgName = org.Name
			activationState = org.ActivationState
			plan = org.Plan
			domainJoin = org.DomainJoin
			primaryDomain = org.PrimaryDomain
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"user_id":          claims.UserID,
			"email":            claims.Email,
			"name":             claims.Name,
			"org_id":           claims.OrgID,
			"org_name":         orgName,
			"role":             claims.Role,
			"activation_state": activationState,
			"plan":             plan,
			"domain_join":      domainJoin,
			"primary_domain":   primaryDomain,
		})
```

- [ ] **Step 4: Run test to verify it passes**

Run: `CGO_ENABLED=0 go test ./ee/auth/... -run TestAuthValidate_IncludesDomainJoinFields -v`
Expected: PASS

- [ ] **Step 5: Run the full package test suite, gofmt, vet, build**

Run: `CGO_ENABLED=0 go test ./ee/auth/... -v && gofmt -w cmd/ pkg/ ee/ && CGO_ENABLED=0 go vet ./... && CGO_ENABLED=0 go build ./...`
Expected: all PASS, clean

- [ ] **Step 6: Commit**

```bash
git add ee/auth/admin.go ee/auth/admin_test.go
git commit -m "$(cat <<'EOF'
feat(auth): add domain_join/primary_domain to /auth/validate

The self-service org-admin page can't call listAdminOrgs (stays
super-admin-only — it lists every org), so /auth/validate is
extended to carry the two fields the new Access tab needs to seed
its domain-join toggle for the caller's own org.
EOF
)"
```

---

### Task 5: Frontend API client — types and new functions

**Files:**
- Modify: `frontend/src/lib/api.ts`

**Interfaces:**
- Consumes: the backend response shapes from Tasks 2–4 (`Organization` JSON now includable via existing `AdminOrg`-shaped responses; `OrgJoinRequest` JSON; the extended `ValidateResponse` JSON).
- Produces: `AdminJoinRequest` type; `AdminOrg.domain_join`, `AdminOrg.primary_domain`, `AdminOrg.created_at?` (now optional); `ValidateResponse.role`, `ValidateResponse.domain_join`, `ValidateResponse.primary_domain`; `client.listJoinRequests(orgId: string)`, `client.approveJoinRequest(orgId: string, reqId: string)`, `client.denyJoinRequest(orgId: string, reqId: string)`, `client.setDomainJoin(orgId: string, domainJoin: boolean)`, `client.renameOrg(orgId: string, name: string)`. Tasks 6–9 use all of these.

No backend to test against here — this task is pure typing plus thin `fetchApi` wrappers matching the existing pattern exactly (see `client.setAdminOrgProviderConfig` for the closest precedent: a `PUT` that returns the updated resource).

- [ ] **Step 1: Extend `ValidateResponse`**

In `frontend/src/lib/api.ts`, replace:

```ts
export interface ValidateResponse {
  user_id: string;
  org_id: string;
  org_name: string;
  activation_state: string;
  plan: string;
}
```

with:

```ts
export interface ValidateResponse {
  user_id: string;
  org_id: string;
  org_name: string;
  activation_state: string;
  plan: string;
  role: string;
  domain_join: boolean;
  primary_domain: string;
}
```

- [ ] **Step 2: Extend `AdminOrg` and add `AdminJoinRequest`**

Replace:

```ts
export interface AdminOrg {
  id: string;
  name: string;
  plan: string;
  activation_state: string;
  created_at: string;
}
```

with:

```ts
export interface AdminOrg {
  id: string;
  name: string;
  plan: string;
  activation_state: string;
  domain_join: boolean;
  primary_domain: string;
  // Omitted when an AdminOrg is built from /auth/validate (self-service),
  // which doesn't return it — OrgManagementPanel never displays it.
  created_at?: string;
}

export interface AdminJoinRequest {
  id: string;
  org_id: string;
  user_email: string;
  status: string;
  created_at: string;
}
```

- [ ] **Step 3: Add the five new `client.*` functions**

**Drift note (found during SDD setup, 2026-08-09):** `main` gained a `getAdminOrgModelUsage` client function (and its backing `AdminOrgModelUsage`/`AdminUsageRow` types) between `getAdminOrgAuditLogs` and `getAdminOrgProviderConfig`, and gained `listAdminUserAPIKeys`/`createAdminUserAPIKey`/`revokeAdminUserAPIKey` (replacing the functions this plan originally expected at those names) elsewhere in the same object. None of that conflicts with this task — anchor on `setAdminOrgProviderConfig` alone, which is untouched, rather than assuming it's still adjacent to `getAdminOrgAuditLogs`.

In `frontend/src/lib/api.ts`, replace:

```ts
  setAdminOrgProviderConfig: (orgId: string, config: Partial<AdminProviderConfig>) => fetchApi<AdminProviderConfig>(`/admin/orgs/${orgId}/provider`, { method: "PUT", body: JSON.stringify(config) }),
```

with:

```ts
  setAdminOrgProviderConfig: (orgId: string, config: Partial<AdminProviderConfig>) => fetchApi<AdminProviderConfig>(`/admin/orgs/${orgId}/provider`, { method: "PUT", body: JSON.stringify(config) }),
  listJoinRequests: (orgId: string) => fetchApi<AdminJoinRequest[]>(`/admin/orgs/${orgId}/join_requests`),
  approveJoinRequest: (orgId: string, reqId: string) => fetchApi<void>(`/admin/orgs/${orgId}/join_requests/${reqId}/approve`, { method: "POST" }),
  denyJoinRequest: (orgId: string, reqId: string) => fetchApi<void>(`/admin/orgs/${orgId}/join_requests/${reqId}/deny`, { method: "POST" }),
  setDomainJoin: (orgId: string, domainJoin: boolean) => fetchApi<AdminOrg>(`/admin/orgs/${orgId}/domain_join`, { method: "PUT", body: JSON.stringify({ domain_join: domainJoin }) }),
  renameOrg: (orgId: string, name: string) => fetchApi<AdminOrg>(`/admin/orgs/${orgId}/name`, { method: "PUT", body: JSON.stringify({ name }) }),
```

- [ ] **Step 4: Type-check**

Run (from `frontend/`): `npm run build`
Expected: succeeds. (It will fail here if any existing caller relied on `AdminOrg.created_at` always being present in a context where it now might not be — check any error output against Task 5's Step 2 change before moving on. `admin/page.tsx`'s use of `org.created_at` is on data from `listAdminOrgs`, which still always returns it, so this should be a non-issue, but confirm.)

- [ ] **Step 5: Commit**

```bash
git add frontend/src/lib/api.ts
git commit -m "$(cat <<'EOF'
feat(frontend): add API client support for org self-service

New types (AdminJoinRequest, extended AdminOrg/ValidateResponse) and
client functions for join-request approval, domain-join toggling,
and org rename — the backend routes already exist, this just wires
the frontend up to call them.
EOF
)"
```

---

### Task 6: Extract `OrgManagementPanel` (pure refactor)

**Drift note (found during SDD setup, 2026-08-09):** this task was originally written against a 305-line version of `admin/orgs/[orgId]/page.tsx`. Two PRs landed on `main` during planning and grew it to 600 lines: a "Usage" tab (provider/model/per-user cost breakdowns, backed by `client.getAdminOrgModelUsage`) and a full inline API-key management UI folded into the Users tab (expand/collapse per user, generate/revoke, via `client.listAdminUserAPIKeys`/`createAdminUserAPIKey`/`revokeAdminUserAPIKey` — these replaced the `listAdminAPIKeys`-style names this plan originally assumed). Below is rewritten against the actual current file — it extracts *four* tabs (Users, Usage, Provider Config, Audit Logs), not three. **Before running Step 1, re-read the live file** (`frontend/src/app/(dashboard)/admin/orgs/[orgId]/page.tsx`) and diff it against the code below — if it has changed again, adapt the extraction to match the live file's actual behavior; the live file is the source of truth, this plan is not.

**Files:**
- Create: `frontend/src/components/OrgManagementPanel.tsx`
- Modify: `frontend/src/app/(dashboard)/admin/orgs/[orgId]/page.tsx`

**Interfaces:**
- Consumes: `client.listAdminOrgUsers`, `client.createAdminOrgUser`, `client.listAdminUserAPIKeys`, `client.createAdminUserAPIKey`, `client.revokeAdminUserAPIKey`, `client.getAdminOrgAuditLogs`, `client.getAdminOrgModelUsage`, `client.getAdminOrgProviderConfig`, `client.setAdminOrgProviderConfig`, `client.renameOrg` (Task 5); types `AdminOrg`, `AdminUser`, `AdminAuditLog`, `AdminProviderConfig`, `AdminAPIKey`, `AdminOrgModelUsage`, `formatTokens`, `providerLabel` (existing + Task 5).
- Produces: `OrgManagementPanel({ org: AdminOrg, onOrgUpdate: (org: AdminOrg) => void })` — a React component. Task 7 adds a fifth tab to it. Task 8 renders it.

This step moves the existing four-tab UI (Users — with its inline API-key management, Usage, Provider Config, Audit Logs) out of the page and into a component that takes `org`/`onOrgUpdate` as props instead of fetching org metadata itself — the two call sites (super-admin page, self-service page in Task 8) obtain that metadata differently. It also adds the inline-rename control in the header. No new tab yet — that's Task 7. Behavior for the four existing tabs must be unchanged; this is a refactor, not a rewrite.

There is no component test framework in this repo (confirmed: no `testing-library`/`jest`/`vitest` in `frontend/package.json`), so this task's "test cycle" is `npm run build` (type-check) plus the manual pass in Task 9.

- [ ] **Step 1: Create `OrgManagementPanel.tsx`**

```tsx
"use client";

import { useEffect, useState, Fragment } from "react";
import { client, type AdminOrg, type AdminUser, type AdminAuditLog, type AdminProviderConfig, type AdminOrgModelUsage, type AdminAPIKey, formatTokens, providerLabel } from "@/lib/api";
import { Loader2, Users, Activity, Settings, Database, Plus, BarChart3, KeyRound, Pencil, Check, X } from "lucide-react";

export function OrgManagementPanel({ org, onOrgUpdate }: { org: AdminOrg; onOrgUpdate: (org: AdminOrg) => void }) {
  const [users, setUsers] = useState<AdminUser[]>([]);
  const [auditLogs, setAuditLogs] = useState<AdminAuditLog[]>([]);
  const [modelUsage, setModelUsage] = useState<AdminOrgModelUsage | null>(null);
  const [loading, setLoading] = useState(true);
  const [activeTab, setActiveTab] = useState<"users" | "usage" | "audit" | "provider">("users");
  const [busy, setBusy] = useState<string | null>(null);

  // New user form
  const [newEmail, setNewEmail] = useState("");
  const [newName, setNewName] = useState("");
  const [newRole, setNewRole] = useState("member");

  // API keys, expanded per user
  const [expandedUserId, setExpandedUserId] = useState<string | null>(null);
  const [keysByUser, setKeysByUser] = useState<Record<string, AdminAPIKey[]>>({});
  const [keysLoading, setKeysLoading] = useState<string | null>(null);
  const [newKey, setNewKey] = useState<{ userId: string; plaintext: string } | null>(null);
  const [copied, setCopied] = useState(false);

  // Provider form
  const [provName, setProvName] = useState("");
  const [provActor, setProvActor] = useState("");
  const [provCritic, setProvCritic] = useState("");
  const [provKey, setProvKey] = useState("");

  // Rename
  const [renaming, setRenaming] = useState(false);
  const [nameDraft, setNameDraft] = useState(org.name);

  useEffect(() => {
    Promise.all([
      client.listAdminOrgUsers(org.id),
      client.getAdminOrgAuditLogs(org.id),
      client.getAdminOrgProviderConfig(org.id).catch(() => null),
      client.getAdminOrgModelUsage(org.id).catch(() => null),
    ]).then(([usrs, logs, prov, usage]) => {
      setUsers(usrs);
      setAuditLogs(logs);
      setModelUsage(usage);

      if (prov) {
        setProvName(prov.provider_name);
        setProvActor(prov.actor_model || "");
        setProvCritic(prov.critic_model || "");
      } else {
        setProvName("anthropic");
      }

      setLoading(false);
    });
  }, [org.id]);

  useEffect(() => {
    setNameDraft(org.name);
  }, [org.name]);

  const handleCreateUser = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!newEmail || !newName) return;

    setBusy("create_user");
    try {
      const u = await client.createAdminOrgUser(org.id, newEmail, newName, newRole);
      setUsers([u, ...users]);
      setNewEmail("");
      setNewName("");
      setNewRole("member");
      alert("User created successfully!");
    } catch (err) {
      alert("Error: " + (err instanceof Error ? err.message : String(err)));
    } finally {
      setBusy(null);
    }
  };

  const toggleKeys = (userId: string) => {
    setNewKey(null);
    if (expandedUserId === userId) {
      setExpandedUserId(null);
      return;
    }
    setExpandedUserId(userId);
    if (!keysByUser[userId]) {
      setKeysLoading(userId);
      client.listAdminUserAPIKeys(org.id, userId)
        .then(keys => setKeysByUser(prev => ({ ...prev, [userId]: keys })))
        .catch(() => setKeysByUser(prev => ({ ...prev, [userId]: [] })))
        .finally(() => setKeysLoading(null));
    }
  };

  const handleGenerateKey = async (userId: string) => {
    const label = prompt("Label for this key (e.g. \"cli\"):", "cli");
    if (label === null) return;

    setBusy(`genkey-${userId}`);
    try {
      const created = await client.createAdminUserAPIKey(org.id, userId, label || "default");
      setNewKey({ userId, plaintext: created.key });
      setCopied(false);
      setKeysByUser(prev => ({
        ...prev,
        [userId]: [
          { id: created.key_id, user_id: userId, label: created.label, created_at: created.created_at, expires_at: created.expires_at ?? undefined },
          ...(prev[userId] ?? []),
        ],
      }));
    } catch (err) {
      alert("Error: " + (err instanceof Error ? err.message : String(err)));
    } finally {
      setBusy(null);
    }
  };

  const handleRevokeKey = async (userId: string, keyId: string) => {
    if (!confirm("Revoke this key? Anything using it will stop working immediately.")) return;

    setBusy(`revoke-${keyId}`);
    try {
      await client.revokeAdminUserAPIKey(org.id, userId, keyId);
      setKeysByUser(prev => ({ ...prev, [userId]: (prev[userId] ?? []).filter(k => k.id !== keyId) }));
    } catch (err) {
      alert("Error: " + (err instanceof Error ? err.message : String(err)));
    } finally {
      setBusy(null);
    }
  };

  const copyKey = (text: string) => {
    navigator.clipboard.writeText(text).then(() => {
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    });
  };

  const handleSaveProvider = async (e: React.FormEvent) => {
    e.preventDefault();

    setBusy("save_provider");
    try {
      const update: Partial<AdminProviderConfig> = {
        provider_name: provName,
        actor_model: provActor,
        critic_model: provCritic,
      };
      if (provKey) {
        update.api_key = provKey;
      }

      await client.setAdminOrgProviderConfig(org.id, update);
      setProvKey(""); // clear key field after save
      alert("Provider configuration updated successfully!");
    } catch (err) {
      alert("Error: " + (err instanceof Error ? err.message : String(err)));
    } finally {
      setBusy(null);
    }
  };

  const handleSaveName = async () => {
    const trimmed = nameDraft.trim();
    if (!trimmed || trimmed === org.name) {
      setRenaming(false);
      setNameDraft(org.name);
      return;
    }
    setBusy("rename");
    try {
      const updated = await client.renameOrg(org.id, trimmed);
      onOrgUpdate(updated);
      setRenaming(false);
    } catch (err) {
      alert("Error: " + (err instanceof Error ? err.message : String(err)));
    } finally {
      setBusy(null);
    }
  };

  if (loading) {
    return <div className="p-8 text-zinc-400 flex items-center gap-2"><Loader2 className="w-4 h-4 animate-spin" /> Loading org details…</div>;
  }

  return (
    <div className="flex flex-col h-full text-white">
      <div className="mb-8">
        <div className="flex items-center justify-between">
          <div>
            {renaming ? (
              <div className="flex items-center gap-2 mb-2">
                <input
                  autoFocus
                  type="text"
                  value={nameDraft}
                  onChange={e => setNameDraft(e.target.value)}
                  onKeyDown={e => { if (e.key === "Enter") handleSaveName(); if (e.key === "Escape") { setRenaming(false); setNameDraft(org.name); } }}
                  className="bg-white/5 border border-white/10 rounded-lg px-3 py-1 text-2xl font-light tracking-tight focus:outline-none focus:border-indigo-500"
                />
                <button onClick={handleSaveName} disabled={busy === "rename"} className="text-green-400 hover:text-green-300">
                  {busy === "rename" ? <Loader2 className="w-5 h-5 animate-spin" /> : <Check className="w-5 h-5" />}
                </button>
                <button onClick={() => { setRenaming(false); setNameDraft(org.name); }} className="text-zinc-400 hover:text-white">
                  <X className="w-5 h-5" />
                </button>
              </div>
            ) : (
              <h1 className="text-3xl font-light tracking-tight mb-2 flex items-center gap-2">
                {org.name}
                <button onClick={() => setRenaming(true)} className="text-zinc-500 hover:text-white transition-colors" title="Rename organization">
                  <Pencil className="w-4 h-4" />
                </button>
              </h1>
            )}
            <p className="text-zinc-400 font-mono text-sm">ID: {org.id} &bull; Plan: {org.plan} &bull; Status: {org.activation_state}</p>
          </div>
        </div>
      </div>

      <div className="flex gap-4 mb-6 border-b border-white/10 pb-4">
        <button
          onClick={() => setActiveTab("users")}
          className={`flex items-center gap-2 px-4 py-2 rounded-lg text-sm font-medium transition-colors ${activeTab === 'users' ? 'bg-white/10 text-white' : 'text-zinc-400 hover:text-white hover:bg-white/5'}`}
        >
          <Users className="w-4 h-4" /> Users
        </button>
        <button
          onClick={() => setActiveTab("usage")}
          className={`flex items-center gap-2 px-4 py-2 rounded-lg text-sm font-medium transition-colors ${activeTab === 'usage' ? 'bg-white/10 text-white' : 'text-zinc-400 hover:text-white hover:bg-white/5'}`}
        >
          <BarChart3 className="w-4 h-4" /> Usage
        </button>
        <button
          onClick={() => setActiveTab("provider")}
          className={`flex items-center gap-2 px-4 py-2 rounded-lg text-sm font-medium transition-colors ${activeTab === 'provider' ? 'bg-white/10 text-white' : 'text-zinc-400 hover:text-white hover:bg-white/5'}`}
        >
          <Database className="w-4 h-4" /> Provider Config
        </button>
        <button
          onClick={() => setActiveTab("audit")}
          className={`flex items-center gap-2 px-4 py-2 rounded-lg text-sm font-medium transition-colors ${activeTab === 'audit' ? 'bg-white/10 text-white' : 'text-zinc-400 hover:text-white hover:bg-white/5'}`}
        >
          <Activity className="w-4 h-4" /> Audit Logs
        </button>
      </div>

      <div className="flex-1 overflow-auto">
        {activeTab === 'users' && (
          <div className="space-y-6">
            <div className="glass-panel p-6 border border-white/10 rounded-xl">
              <h2 className="text-lg font-medium mb-4 flex items-center gap-2">
                <Plus className="w-5 h-5" /> Add User
              </h2>
              <form onSubmit={handleCreateUser} className="flex gap-4 items-end">
                <div className="flex-1">
                  <label className="block text-xs text-zinc-400 mb-1">Name</label>
                  <input type="text" value={newName} onChange={e => setNewName(e.target.value)} required className="w-full bg-white/5 border border-white/10 rounded-lg px-3 py-2 text-sm focus:outline-none focus:border-indigo-500" placeholder="John Doe" />
                </div>
                <div className="flex-1">
                  <label className="block text-xs text-zinc-400 mb-1">Email</label>
                  <input type="email" value={newEmail} onChange={e => setNewEmail(e.target.value)} required className="w-full bg-white/5 border border-white/10 rounded-lg px-3 py-2 text-sm focus:outline-none focus:border-indigo-500" placeholder="john@example.com" />
                </div>
                <div className="w-32">
                  <label className="block text-xs text-zinc-400 mb-1">Role</label>
                  <select value={newRole} onChange={e => setNewRole(e.target.value)} className="w-full bg-[#1c1c1c] border border-white/10 rounded-lg px-3 py-2 text-sm focus:outline-none focus:border-indigo-500">
                    <option value="member">Member</option>
                    <option value="admin">Admin</option>
                  </select>
                </div>
                <button type="submit" disabled={!!busy} className="bg-indigo-600 hover:bg-indigo-700 text-white px-4 py-2 rounded-lg text-sm font-medium transition-colors h-[38px] flex items-center justify-center min-w-[100px]">
                  {busy === 'create_user' ? <Loader2 className="w-4 h-4 animate-spin" /> : 'Create User'}
                </button>
              </form>
            </div>

            <div className="glass-panel border border-white/10 rounded-xl overflow-hidden">
              <div className="overflow-x-auto">
              <table className="w-full text-sm text-left">
                <thead className="bg-white/5 border-b border-white/10 text-xs font-medium text-zinc-400">
                  <tr>
                    <th className="px-4 py-3">Name</th>
                    <th className="px-4 py-3">Email</th>
                    <th className="px-4 py-3">Role</th>
                    <th className="px-4 py-3">Joined</th>
                    <th className="px-4 py-3 text-right">API Keys</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-white/5">
                  {users.map(user => (
                    <Fragment key={user.id}>
                      <tr className="hover:bg-white/[0.02] transition-colors">
                        <td className="px-4 py-3 font-medium">{user.name}</td>
                        <td className="px-4 py-3 text-zinc-300">{user.email}</td>
                        <td className="px-4 py-3">
                          <span className={`inline-flex px-2 py-0.5 rounded text-xs ${user.role === 'admin' ? 'bg-indigo-500/10 text-indigo-400' : 'bg-white/10 text-zinc-300'}`}>
                            {user.role}
                          </span>
                        </td>
                        <td className="px-4 py-3 text-zinc-400">{new Date(user.created_at).toLocaleDateString()}</td>
                        <td className="px-4 py-3 text-right">
                          <button
                            onClick={() => toggleKeys(user.id)}
                            className="inline-flex items-center gap-1 text-xs bg-white/5 hover:bg-white/10 border border-white/10 rounded px-2 py-1 transition-colors"
                          >
                            <KeyRound className="w-3 h-3" /> Keys
                          </button>
                        </td>
                      </tr>
                      {expandedUserId === user.id && (
                        <tr>
                          <td colSpan={5} className="px-4 py-4 bg-black/20">
                            <div className="flex items-center justify-between mb-3">
                              <h3 className="text-xs font-bold text-zinc-500 uppercase tracking-widest">API Keys for {user.email}</h3>
                              <button
                                onClick={() => handleGenerateKey(user.id)}
                                disabled={busy === `genkey-${user.id}`}
                                className="flex items-center gap-1 text-xs bg-indigo-500/10 hover:bg-indigo-500/20 text-indigo-400 border border-indigo-500/20 rounded px-2 py-1 transition-colors"
                              >
                                {busy === `genkey-${user.id}` ? <Loader2 className="w-3 h-3 animate-spin" /> : <Plus className="w-3 h-3" />}
                                Generate Key
                              </button>
                            </div>

                            {newKey && newKey.userId === user.id && (
                              <div className="mb-3 p-3 rounded-lg border border-amber-500/30 bg-amber-500/5">
                                <p className="text-xs text-amber-400 mb-2">
                                  Shown once — copy it now. It is not stored in plaintext and cannot be retrieved again, only revoked.
                                </p>
                                <div className="flex items-center gap-2">
                                  <code className="flex-1 text-xs font-mono text-white break-all bg-black/30 px-2 py-1.5 rounded">
                                    {newKey.plaintext}
                                  </code>
                                  <button
                                    onClick={() => copyKey(newKey.plaintext)}
                                    className="text-xs bg-white/10 hover:bg-white/20 rounded px-2 py-1.5 shrink-0 transition-colors"
                                  >
                                    {copied ? "Copied!" : "Copy"}
                                  </button>
                                </div>
                              </div>
                            )}

                            {keysLoading === user.id ? (
                              <div className="text-xs text-zinc-500">Loading keys…</div>
                            ) : (
                              <div className="overflow-x-auto">
                              <table className="w-full text-xs text-left">
                                <thead className="text-zinc-500">
                                  <tr>
                                    <th className="py-1 pr-4 font-medium">Label</th>
                                    <th className="py-1 pr-4 font-medium">Created</th>
                                    <th className="py-1 pr-4 font-medium">Expires</th>
                                    <th className="py-1 text-right font-medium">Action</th>
                                  </tr>
                                </thead>
                                <tbody className="divide-y divide-white/5">
                                  {(keysByUser[user.id] ?? []).map(key => (
                                    <tr key={key.id}>
                                      <td className="py-1.5 pr-4">{key.label || "default"}</td>
                                      <td className="py-1.5 pr-4 text-zinc-400">{new Date(key.created_at).toLocaleDateString()}</td>
                                      <td className="py-1.5 pr-4 text-zinc-400">{key.expires_at ? new Date(key.expires_at).toLocaleDateString() : "Never"}</td>
                                      <td className="py-1.5 text-right">
                                        <button
                                          onClick={() => handleRevokeKey(user.id, key.id)}
                                          disabled={busy === `revoke-${key.id}`}
                                          className="text-red-400 hover:text-red-300 transition-colors"
                                        >
                                          {busy === `revoke-${key.id}` ? <Loader2 className="w-3 h-3 animate-spin inline" /> : "Revoke"}
                                        </button>
                                      </td>
                                    </tr>
                                  ))}
                                  {(keysByUser[user.id] ?? []).length === 0 && (
                                    <tr>
                                      <td colSpan={4} className="py-2 text-zinc-500">No active keys.</td>
                                    </tr>
                                  )}
                                </tbody>
                              </table>
                              </div>
                            )}
                          </td>
                        </tr>
                      )}
                    </Fragment>
                  ))}
                  {users.length === 0 && (
                    <tr>
                      <td colSpan={5} className="px-4 py-8 text-center text-zinc-500">No users found.</td>
                    </tr>
                  )}
                </tbody>
              </table>
              </div>
            </div>
          </div>
        )}

        {activeTab === 'usage' && (
          <div className="space-y-6">
            {modelUsage && Object.keys(modelUsage.tasks_by_status).length > 0 && (
              <div className="glass-panel p-5 border border-white/10 rounded-xl">
                <h2 className="text-xs font-bold text-zinc-500 uppercase tracking-widest mb-3">Task Queue</h2>
                <div className="flex gap-6">
                  {Object.entries(modelUsage.tasks_by_status).map(([status, count]) => (
                    <div key={status} className="flex items-baseline gap-2">
                      <span className="text-2xl font-light">{count}</span>
                      <span className="text-xs text-zinc-400">{status}</span>
                    </div>
                  ))}
                </div>
              </div>
            )}

            <div className="grid grid-cols-1 lg:grid-cols-2 gap-4">
              <div className="glass-panel border border-white/10 rounded-xl overflow-hidden">
                <h2 className="text-xs font-bold text-zinc-500 uppercase tracking-widest px-4 pt-4 pb-3">
                  Usage by Provider
                </h2>
                <div className="overflow-x-auto">
                <table className="w-full text-sm text-left">
                  <thead className="bg-white/5 border-b border-white/10 text-xs font-medium text-zinc-400">
                    <tr>
                      <th className="px-4 py-2">Provider</th>
                      <th className="px-4 py-2 text-right">Tasks</th>
                      <th className="px-4 py-2 text-right">Cost</th>
                      <th className="px-4 py-2 text-right">Kiwi-funded</th>
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-white/5">
                    {(modelUsage?.provider_usage ?? []).map((row) => (
                      <tr key={row.provider} className="hover:bg-white/[0.02] transition-colors">
                        <td className="px-4 py-2 font-medium">{providerLabel(row.provider)}</td>
                        <td className="px-4 py-2 text-right text-zinc-300">{row.task_count}</td>
                        <td className="px-4 py-2 text-right">${row.cost_usd.toFixed(2)}</td>
                        <td className="px-4 py-2 text-right text-zinc-400">${row.kiwi_cost_usd.toFixed(2)}</td>
                      </tr>
                    ))}
                    {(!modelUsage || modelUsage.provider_usage.length === 0) && (
                      <tr>
                        <td colSpan={4} className="px-4 py-8 text-center text-zinc-500">No usage recorded yet.</td>
                      </tr>
                    )}
                  </tbody>
                </table>
                </div>
              </div>
              <div className="glass-panel border border-white/10 rounded-xl overflow-hidden">
                <h2 className="text-xs font-bold text-zinc-500 uppercase tracking-widest px-4 pt-4 pb-3">
                  Usage by Model
                </h2>
                <div className="overflow-x-auto">
                <table className="w-full text-sm text-left">
                  <thead className="bg-white/5 border-b border-white/10 text-xs font-medium text-zinc-400">
                    <tr>
                      <th className="px-4 py-2">Model</th>
                      <th className="px-4 py-2 text-right">Tasks</th>
                      <th className="px-4 py-2 text-right">Cost</th>
                      <th className="px-4 py-2 text-right">Tokens</th>
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-white/5">
                    {(modelUsage?.model_usage ?? []).map((row) => (
                      <tr key={row.model} className="hover:bg-white/[0.02] transition-colors">
                        <td className="px-4 py-2 font-medium font-mono text-xs">{row.model}</td>
                        <td className="px-4 py-2 text-right text-zinc-300">{row.task_count}</td>
                        <td className="px-4 py-2 text-right">${row.cost_usd.toFixed(2)}</td>
                        <td className="px-4 py-2 text-right text-zinc-400">
                          {formatTokens(row.tokens_in)} in / {formatTokens(row.tokens_out)} out
                        </td>
                      </tr>
                    ))}
                    {(!modelUsage || modelUsage.model_usage.length === 0) && (
                      <tr>
                        <td colSpan={4} className="px-4 py-8 text-center text-zinc-500">No usage recorded yet.</td>
                      </tr>
                    )}
                  </tbody>
                </table>
                </div>
              </div>
            </div>

            <div className="glass-panel border border-white/10 rounded-xl overflow-hidden">
              <h2 className="text-xs font-bold text-zinc-500 uppercase tracking-widest px-4 pt-4 pb-3">
                Usage by User
              </h2>
              <div className="overflow-x-auto">
              <table className="w-full text-sm text-left">
                <thead className="bg-white/5 border-b border-white/10 text-xs font-medium text-zinc-400">
                  <tr>
                    <th className="px-4 py-2">User</th>
                    <th className="px-4 py-2 text-right">Tasks</th>
                    <th className="px-4 py-2 text-right">Succeeded</th>
                    <th className="px-4 py-2 text-right">Failed</th>
                    <th className="px-4 py-2 text-right">Cost</th>
                    <th className="px-4 py-2 text-right">Kiwi-funded</th>
                    <th className="px-4 py-2 text-right">Tokens</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-white/5">
                  {(modelUsage?.per_user ?? []).map((row) => (
                    <tr key={row.user_id} className="hover:bg-white/[0.02] transition-colors">
                      <td className="px-4 py-2">
                        <div className="font-medium">{row.email || row.user_id}</div>
                        <div className="text-[10px] text-zinc-500 font-mono">{row.user_id}</div>
                      </td>
                      <td className="px-4 py-2 text-right text-zinc-300">{row.task_count}</td>
                      <td className="px-4 py-2 text-right text-green-400">{row.succeeded}</td>
                      <td className="px-4 py-2 text-right text-red-400">{row.failed}</td>
                      <td className="px-4 py-2 text-right">${row.cost_usd.toFixed(2)}</td>
                      <td className="px-4 py-2 text-right text-zinc-400">${row.kiwi_cost_usd.toFixed(2)}</td>
                      <td className="px-4 py-2 text-right text-zinc-400">
                        {formatTokens(row.tokens_in)} in / {formatTokens(row.tokens_out)} out
                      </td>
                    </tr>
                  ))}
                  {(!modelUsage || modelUsage.per_user.length === 0) && (
                    <tr>
                      <td colSpan={7} className="px-4 py-8 text-center text-zinc-500">No usage recorded yet.</td>
                    </tr>
                  )}
                </tbody>
              </table>
              </div>
            </div>
          </div>
        )}

        {activeTab === 'provider' && (
          <div className="glass-panel p-6 border border-white/10 rounded-xl max-w-2xl">
            <h2 className="text-lg font-medium mb-4 flex items-center gap-2">
              <Settings className="w-5 h-5" /> LLM Provider Override
            </h2>
            <p className="text-sm text-zinc-400 mb-6">
              Configure custom LLM provider settings for this organization. This will override global defaults.
            </p>
            <form onSubmit={handleSaveProvider} className="space-y-4">
              <div>
                <label className="block text-sm font-medium text-zinc-300 mb-1">Provider Name</label>
                <select value={provName} onChange={e => setProvName(e.target.value)} className="w-full bg-[#1c1c1c] border border-white/10 rounded-lg px-3 py-2 text-sm focus:outline-none focus:border-indigo-500">
                  <option value="anthropic">Anthropic</option>
                  <option value="openai">OpenAI</option>
                  <option value="gemini">Gemini</option>
                </select>
              </div>
              <div>
                <label className="block text-sm font-medium text-zinc-300 mb-1">API Key</label>
                <input type="password" value={provKey} onChange={e => setProvKey(e.target.value)} className="w-full bg-white/5 border border-white/10 rounded-lg px-3 py-2 text-sm focus:outline-none focus:border-indigo-500" placeholder="Leave blank to keep existing key" />
                <p className="text-xs text-zinc-500 mt-1">Stored securely. Only enter a new key to update.</p>
              </div>
              <div className="grid grid-cols-2 gap-4">
                <div>
                  <label className="block text-sm font-medium text-zinc-300 mb-1">Actor Model</label>
                  <input type="text" value={provActor} onChange={e => setProvActor(e.target.value)} className="w-full bg-white/5 border border-white/10 rounded-lg px-3 py-2 text-sm focus:outline-none focus:border-indigo-500" placeholder="e.g. claude-3-5-sonnet-20241022" />
                </div>
                <div>
                  <label className="block text-sm font-medium text-zinc-300 mb-1">Critic Model</label>
                  <input type="text" value={provCritic} onChange={e => setProvCritic(e.target.value)} className="w-full bg-white/5 border border-white/10 rounded-lg px-3 py-2 text-sm focus:outline-none focus:border-indigo-500" placeholder="e.g. claude-3-5-haiku-20241022" />
                </div>
              </div>
              <div className="pt-4 border-t border-white/10 mt-6">
                <button type="submit" disabled={!!busy} className="bg-indigo-600 hover:bg-indigo-700 text-white px-6 py-2 rounded-lg text-sm font-medium transition-colors flex items-center justify-center min-w-[120px]">
                  {busy === 'save_provider' ? <Loader2 className="w-4 h-4 animate-spin" /> : 'Save Config'}
                </button>
              </div>
            </form>
          </div>
        )}

        {activeTab === 'audit' && (
          <div className="glass-panel border border-white/10 rounded-xl overflow-hidden">
            <div className="overflow-x-auto">
            <table className="w-full text-sm text-left">
              <thead className="bg-white/5 border-b border-white/10 text-xs font-medium text-zinc-400">
                <tr>
                  <th className="px-4 py-3">Timestamp</th>
                  <th className="px-4 py-3">User</th>
                  <th className="px-4 py-3">Action</th>
                  <th className="px-4 py-3">Resource</th>
                  <th className="px-4 py-3">Details</th>
                  <th className="px-4 py-3">IP Address</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-white/5">
                {auditLogs.map(log => (
                  <tr key={log.id} className="hover:bg-white/[0.02] transition-colors">
                    <td className="px-4 py-3 text-zinc-400 whitespace-nowrap">{new Date(log.created_at).toLocaleString()}</td>
                    <td className="px-4 py-3">
                      <div className="font-medium">{log.user_email || 'System'}</div>
                      {log.user_id && <div className="text-[10px] text-zinc-500 font-mono">{log.user_id}</div>}
                    </td>
                    <td className="px-4 py-3">
                      <span className="inline-flex px-2 py-0.5 rounded text-xs bg-white/10 text-zinc-300 font-mono">
                        {log.action}
                      </span>
                    </td>
                    <td className="px-4 py-3">
                      <div className="font-mono text-zinc-300">{log.resource}</div>
                      <div className="text-[10px] text-zinc-500 font-mono truncate max-w-[120px]">{log.resource_id}</div>
                    </td>
                    <td className="px-4 py-3 text-zinc-300 truncate max-w-md">{log.details}</td>
                    <td className="px-4 py-3 text-zinc-500 font-mono text-xs">{log.client_ip || '-'}</td>
                  </tr>
                ))}
                {auditLogs.length === 0 && (
                  <tr>
                    <td colSpan={6} className="px-4 py-8 text-center text-zinc-500">No audit logs found.</td>
                  </tr>
                )}
              </tbody>
            </table>
            </div>
          </div>
        )}
      </div>
    </div>
  );
}
```

- [ ] **Step 2: Rewire the super-admin page to use it**

Replace the full contents of `frontend/src/app/(dashboard)/admin/orgs/[orgId]/page.tsx` with:

```tsx
"use client";

import { useEffect, useState, use } from "react";
import { useRouter } from "next/navigation";
import { client, type AdminOrg } from "@/lib/api";
import { ArrowLeft } from "lucide-react";
import { LoadingState } from "@/components/LoadingState";
import { OrgManagementPanel } from "@/components/OrgManagementPanel";
import Link from "next/link";

export default function AdminOrgPage({ params }: { params: Promise<{ orgId: string }> }) {
  const router = useRouter();
  const unwrappedParams = use(params);
  const orgId = unwrappedParams.orgId;

  const [org, setOrg] = useState<AdminOrg | null>(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    client.getUsage().then(u => {
      if (!u.is_super_admin) {
        router.push("/");
        return;
      }
      return client.listAdminOrgs().then(orgs => orgs.find(o => o.id === orgId) || null).then(o => {
        if (!o) {
          router.push("/admin");
          return;
        }
        setOrg(o);
        setLoading(false);
      });
    }).catch(() => {
      router.push("/");
    });
  }, [router, orgId]);

  if (loading || !org) {
    return <LoadingState label="Loading org details…" className="h-full" />;
  }

  return (
    <div className="p-8 max-w-7xl mx-auto h-full flex flex-col text-white">
      <Link href="/admin" className="text-sm text-zinc-400 hover:text-white flex items-center gap-1 mb-4 w-fit">
        <ArrowLeft className="w-4 h-4" /> Back to Admin
      </Link>
      <OrgManagementPanel org={org} onOrgUpdate={setOrg} />
    </div>
  );
}
```

- [ ] **Step 3: Type-check**

Run (from `frontend/`): `npm run build`
Expected: succeeds, no type errors.

- [ ] **Step 4: Commit**

```bash
git add frontend/src/components/OrgManagementPanel.tsx "frontend/src/app/(dashboard)/admin/orgs/[orgId]/page.tsx"
git commit -m "$(cat <<'EOF'
refactor(frontend): extract OrgManagementPanel from admin org page

Pure extraction — same four tabs (Users with inline API-key
management, Usage, Provider Config, Audit Logs), same behavior —
plus an inline-rename control on the header (new: no rename endpoint
existed before this feature). Takes org/onOrgUpdate as props instead
of fetching org metadata itself, since the upcoming self-service page
can't call the super-admin-only listAdminOrgs the old page used.
EOF
)"
```

---

### Task 7: Add the Access tab (join requests + domain-join)

**Drift note (found during SDD setup, 2026-08-09):** this task's before/after snippets are rewritten to match Task 6's rewritten output — a `modelUsage` state and a `usage` tab exist where this task originally expected a `providerConfig` state (the real `OrgManagementPanel` never stores the provider config response; it only seeds the form on load). The `"access"` tab is now a fifth tab, appended after the (unchanged) Audit Logs button.

**Files:**
- Modify: `frontend/src/components/OrgManagementPanel.tsx`

**Interfaces:**
- Consumes: `client.listJoinRequests`, `client.approveJoinRequest`, `client.denyJoinRequest`, `client.setDomainJoin` (Task 5); `AdminJoinRequest` type (Task 5).
- Produces: nothing new downstream — this completes `OrgManagementPanel` for Task 8.

This is the UI that has never existed before, for anyone — no join-request or domain-join surface exists in the frontend today, super-admin included.

- [ ] **Step 1: Add state and data fetching for the Access tab**

In `frontend/src/components/OrgManagementPanel.tsx`, update the tab type and add join-request state. Replace:

```tsx
import { client, type AdminOrg, type AdminUser, type AdminAuditLog, type AdminProviderConfig, type AdminOrgModelUsage, type AdminAPIKey, formatTokens, providerLabel } from "@/lib/api";
import { Loader2, Users, Activity, Settings, Database, Plus, BarChart3, KeyRound, Pencil, Check, X } from "lucide-react";

export function OrgManagementPanel({ org, onOrgUpdate }: { org: AdminOrg; onOrgUpdate: (org: AdminOrg) => void }) {
  const [users, setUsers] = useState<AdminUser[]>([]);
  const [auditLogs, setAuditLogs] = useState<AdminAuditLog[]>([]);
  const [modelUsage, setModelUsage] = useState<AdminOrgModelUsage | null>(null);
  const [loading, setLoading] = useState(true);
  const [activeTab, setActiveTab] = useState<"users" | "usage" | "audit" | "provider">("users");
  const [busy, setBusy] = useState<string | null>(null);
```

with:

```tsx
import { client, type AdminOrg, type AdminUser, type AdminAuditLog, type AdminProviderConfig, type AdminOrgModelUsage, type AdminAPIKey, type AdminJoinRequest, formatTokens, providerLabel } from "@/lib/api";
import { Loader2, Users, Activity, Settings, Database, Plus, BarChart3, KeyRound, Pencil, Check, X, ShieldCheck } from "lucide-react";

export function OrgManagementPanel({ org, onOrgUpdate }: { org: AdminOrg; onOrgUpdate: (org: AdminOrg) => void }) {
  const [users, setUsers] = useState<AdminUser[]>([]);
  const [auditLogs, setAuditLogs] = useState<AdminAuditLog[]>([]);
  const [modelUsage, setModelUsage] = useState<AdminOrgModelUsage | null>(null);
  const [joinRequests, setJoinRequests] = useState<AdminJoinRequest[]>([]);
  const [loading, setLoading] = useState(true);
  const [activeTab, setActiveTab] = useState<"users" | "usage" | "audit" | "provider" | "access">("users");
  const [busy, setBusy] = useState<string | null>(null);
```

Replace the data-fetching `useEffect`:

```tsx
  useEffect(() => {
    Promise.all([
      client.listAdminOrgUsers(org.id),
      client.getAdminOrgAuditLogs(org.id),
      client.getAdminOrgProviderConfig(org.id).catch(() => null),
      client.getAdminOrgModelUsage(org.id).catch(() => null),
    ]).then(([usrs, logs, prov, usage]) => {
      setUsers(usrs);
      setAuditLogs(logs);
      setModelUsage(usage);

      if (prov) {
        setProvName(prov.provider_name);
        setProvActor(prov.actor_model || "");
        setProvCritic(prov.critic_model || "");
      } else {
        setProvName("anthropic");
      }

      setLoading(false);
    });
  }, [org.id]);
```

with:

```tsx
  useEffect(() => {
    Promise.all([
      client.listAdminOrgUsers(org.id),
      client.getAdminOrgAuditLogs(org.id),
      client.getAdminOrgProviderConfig(org.id).catch(() => null),
      client.getAdminOrgModelUsage(org.id).catch(() => null),
      client.listJoinRequests(org.id).catch(() => []),
    ]).then(([usrs, logs, prov, usage, reqs]) => {
      setUsers(usrs);
      setAuditLogs(logs);
      setModelUsage(usage);
      setJoinRequests(reqs);

      if (prov) {
        setProvName(prov.provider_name);
        setProvActor(prov.actor_model || "");
        setProvCritic(prov.critic_model || "");
      } else {
        setProvName("anthropic");
      }

      setLoading(false);
    });
  }, [org.id]);
```

- [ ] **Step 2: Add the handlers**

After `handleSaveName` (added in Task 6), add:

```tsx
  const handleToggleDomainJoin = async () => {
    setBusy("domain_join");
    try {
      const updated = await client.setDomainJoin(org.id, !org.domain_join);
      onOrgUpdate(updated);
    } catch (err: any) {
      alert("Error: " + err.message);
    } finally {
      setBusy(null);
    }
  };

  const handleApproveJoinRequest = async (reqId: string) => {
    setBusy(`approve-${reqId}`);
    try {
      await client.approveJoinRequest(org.id, reqId);
      setJoinRequests(joinRequests.filter(r => r.id !== reqId));
    } catch (err: any) {
      alert("Error: " + err.message);
    } finally {
      setBusy(null);
    }
  };

  const handleDenyJoinRequest = async (reqId: string) => {
    setBusy(`deny-${reqId}`);
    try {
      await client.denyJoinRequest(org.id, reqId);
      setJoinRequests(joinRequests.filter(r => r.id !== reqId));
    } catch (err: any) {
      alert("Error: " + err.message);
    } finally {
      setBusy(null);
    }
  };
```

- [ ] **Step 3: Add the tab button and tab content**

Replace:

```tsx
        <button
          onClick={() => setActiveTab("audit")}
          className={`flex items-center gap-2 px-4 py-2 rounded-lg text-sm font-medium transition-colors ${activeTab === 'audit' ? 'bg-white/10 text-white' : 'text-zinc-400 hover:text-white hover:bg-white/5'}`}
        >
          <Activity className="w-4 h-4" /> Audit Logs
        </button>
      </div>
```

with:

```tsx
        <button
          onClick={() => setActiveTab("audit")}
          className={`flex items-center gap-2 px-4 py-2 rounded-lg text-sm font-medium transition-colors ${activeTab === 'audit' ? 'bg-white/10 text-white' : 'text-zinc-400 hover:text-white hover:bg-white/5'}`}
        >
          <Activity className="w-4 h-4" /> Audit Logs
        </button>
        <button
          onClick={() => setActiveTab("access")}
          className={`flex items-center gap-2 px-4 py-2 rounded-lg text-sm font-medium transition-colors ${activeTab === 'access' ? 'bg-white/10 text-white' : 'text-zinc-400 hover:text-white hover:bg-white/5'}`}
        >
          <ShieldCheck className="w-4 h-4" /> Access
        </button>
      </div>
```

Replace the closing of the tab content area:

```tsx
        {activeTab === 'audit' && (
          <div className="glass-panel border border-white/10 rounded-xl overflow-hidden">
            <div className="overflow-x-auto">
            <table className="w-full text-sm text-left">
              <thead className="bg-white/5 border-b border-white/10 text-xs font-medium text-zinc-400">
                <tr>
                  <th className="px-4 py-3">Timestamp</th>
                  <th className="px-4 py-3">User</th>
                  <th className="px-4 py-3">Action</th>
                  <th className="px-4 py-3">Resource</th>
                  <th className="px-4 py-3">Details</th>
                  <th className="px-4 py-3">IP Address</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-white/5">
                {auditLogs.map(log => (
                  <tr key={log.id} className="hover:bg-white/[0.02] transition-colors">
                    <td className="px-4 py-3 text-zinc-400 whitespace-nowrap">{new Date(log.created_at).toLocaleString()}</td>
                    <td className="px-4 py-3">
                      <div className="font-medium">{log.user_email || 'System'}</div>
                      {log.user_id && <div className="text-[10px] text-zinc-500 font-mono">{log.user_id}</div>}
                    </td>
                    <td className="px-4 py-3">
                      <span className="inline-flex px-2 py-0.5 rounded text-xs bg-white/10 text-zinc-300 font-mono">
                        {log.action}
                      </span>
                    </td>
                    <td className="px-4 py-3">
                      <div className="font-mono text-zinc-300">{log.resource}</div>
                      <div className="text-[10px] text-zinc-500 font-mono truncate max-w-[120px]">{log.resource_id}</div>
                    </td>
                    <td className="px-4 py-3 text-zinc-300 truncate max-w-md">{log.details}</td>
                    <td className="px-4 py-3 text-zinc-500 font-mono text-xs">{log.client_ip || '-'}</td>
                  </tr>
                ))}
                {auditLogs.length === 0 && (
                  <tr>
                    <td colSpan={6} className="px-4 py-8 text-center text-zinc-500">No audit logs found.</td>
                  </tr>
                )}
              </tbody>
            </table>
            </div>
          </div>
        )}
      </div>
    </div>
  );
}
```

with:

```tsx
        {activeTab === 'audit' && (
          <div className="glass-panel border border-white/10 rounded-xl overflow-hidden">
            <div className="overflow-x-auto">
            <table className="w-full text-sm text-left">
              <thead className="bg-white/5 border-b border-white/10 text-xs font-medium text-zinc-400">
                <tr>
                  <th className="px-4 py-3">Timestamp</th>
                  <th className="px-4 py-3">User</th>
                  <th className="px-4 py-3">Action</th>
                  <th className="px-4 py-3">Resource</th>
                  <th className="px-4 py-3">Details</th>
                  <th className="px-4 py-3">IP Address</th>
                </tr>
              </thead>
              <tbody className="divide-y divide-white/5">
                {auditLogs.map(log => (
                  <tr key={log.id} className="hover:bg-white/[0.02] transition-colors">
                    <td className="px-4 py-3 text-zinc-400 whitespace-nowrap">{new Date(log.created_at).toLocaleString()}</td>
                    <td className="px-4 py-3">
                      <div className="font-medium">{log.user_email || 'System'}</div>
                      {log.user_id && <div className="text-[10px] text-zinc-500 font-mono">{log.user_id}</div>}
                    </td>
                    <td className="px-4 py-3">
                      <span className="inline-flex px-2 py-0.5 rounded text-xs bg-white/10 text-zinc-300 font-mono">
                        {log.action}
                      </span>
                    </td>
                    <td className="px-4 py-3">
                      <div className="font-mono text-zinc-300">{log.resource}</div>
                      <div className="text-[10px] text-zinc-500 font-mono truncate max-w-[120px]">{log.resource_id}</div>
                    </td>
                    <td className="px-4 py-3 text-zinc-300 truncate max-w-md">{log.details}</td>
                    <td className="px-4 py-3 text-zinc-500 font-mono text-xs">{log.client_ip || '-'}</td>
                  </tr>
                ))}
                {auditLogs.length === 0 && (
                  <tr>
                    <td colSpan={6} className="px-4 py-8 text-center text-zinc-500">No audit logs found.</td>
                  </tr>
                )}
              </tbody>
            </table>
            </div>
          </div>
        )}

        {activeTab === 'access' && (
          <div className="space-y-6">
            <div className="glass-panel p-6 border border-white/10 rounded-xl max-w-2xl">
              <h2 className="text-lg font-medium mb-2 flex items-center gap-2">
                <ShieldCheck className="w-5 h-5" /> Domain join
              </h2>
              <p className="text-sm text-zinc-400 mb-4">
                {org.primary_domain
                  ? `When on, anyone signing up with an @${org.primary_domain} email joins this org immediately, without approval.`
                  : "This org has no primary domain set — domain join has no effect until one is configured."}
              </p>
              <button
                type="button"
                role="switch"
                aria-checked={org.domain_join}
                disabled={busy === "domain_join"}
                onClick={handleToggleDomainJoin}
                className={`flex items-center gap-2 px-4 py-2 rounded-xl text-xs font-semibold border transition-all disabled:opacity-40 disabled:cursor-not-allowed ${
                  org.domain_join
                    ? "border-green-500/40 bg-green-500/20 text-green-300"
                    : "border-white/10 bg-white/5 text-zinc-400 hover:text-white"
                }`}
              >
                {busy === "domain_join" ? <Loader2 className="w-4 h-4 animate-spin" /> : null}
                <span>{org.domain_join ? "On" : "Off"}</span>
              </button>
            </div>

            <div className="glass-panel border border-white/10 rounded-xl overflow-hidden">
              <div className="px-6 py-4 border-b border-white/10">
                <h2 className="text-lg font-medium">Pending join requests</h2>
              </div>
              <div className="overflow-x-auto">
              <table className="w-full text-sm text-left">
                <thead className="bg-white/5 border-b border-white/10 text-xs font-medium text-zinc-400">
                  <tr>
                    <th className="px-4 py-3">Email</th>
                    <th className="px-4 py-3">Requested</th>
                    <th className="px-4 py-3 text-right">Actions</th>
                  </tr>
                </thead>
                <tbody className="divide-y divide-white/5">
                  {joinRequests.map(req => (
                    <tr key={req.id} className="hover:bg-white/[0.02] transition-colors">
                      <td className="px-4 py-3 font-medium">{req.user_email}</td>
                      <td className="px-4 py-3 text-zinc-400">{new Date(req.created_at).toLocaleDateString()}</td>
                      <td className="px-4 py-3 text-right space-x-2">
                        <button
                          onClick={() => handleApproveJoinRequest(req.id)}
                          disabled={!!busy}
                          className="text-xs bg-green-500/10 hover:bg-green-500/20 border border-green-500/20 text-green-400 rounded px-2 py-1 transition-colors"
                        >
                          {busy === `approve-${req.id}` ? <Loader2 className="w-3 h-3 animate-spin inline" /> : 'Approve'}
                        </button>
                        <button
                          onClick={() => handleDenyJoinRequest(req.id)}
                          disabled={!!busy}
                          className="text-xs bg-red-500/10 hover:bg-red-500/20 border border-red-500/20 text-red-400 rounded px-2 py-1 transition-colors"
                        >
                          {busy === `deny-${req.id}` ? <Loader2 className="w-3 h-3 animate-spin inline" /> : 'Deny'}
                        </button>
                      </td>
                    </tr>
                  ))}
                  {joinRequests.length === 0 && (
                    <tr>
                      <td colSpan={3} className="px-4 py-8 text-center text-zinc-500">No pending join requests.</td>
                    </tr>
                  )}
                </tbody>
              </table>
              </div>
            </div>
          </div>
        )}
      </div>
    </div>
  );
}
```

- [ ] **Step 4: Type-check**

Run (from `frontend/`): `npm run build`
Expected: succeeds, no type errors.

- [ ] **Step 5: Commit**

```bash
git add frontend/src/components/OrgManagementPanel.tsx
git commit -m "$(cat <<'EOF'
feat(frontend): add Access tab for domain-join and join requests

No UI for either has ever existed, super-admin included. Lands in
the shared OrgManagementPanel so both the existing super-admin org
page and the upcoming self-service /team page get it for free.
EOF
)"
```

---

### Task 8: Nav item and the new self-service `/team` page

**Files:**
- Create: `frontend/src/app/(dashboard)/team/page.tsx`
- Modify: `frontend/src/app/(dashboard)/layout.tsx`

**Interfaces:**
- Consumes: `client.validate()` (extended in Task 4/5 to return `role`, `domain_join`, `primary_domain`); `OrgManagementPanel` (Tasks 6–7); `client.getUsage()` (existing, for `is_super_admin`).
- Produces: nothing downstream — this is the last frontend task before manual verification.

- [ ] **Step 1: Create the `/team` page**

```tsx
"use client";

import { useEffect, useState } from "react";
import { useRouter } from "next/navigation";
import { client, type AdminOrg } from "@/lib/api";
import { LoadingState } from "@/components/LoadingState";
import { OrgManagementPanel } from "@/components/OrgManagementPanel";

export default function TeamPage() {
  const router = useRouter();
  const [org, setOrg] = useState<AdminOrg | null>(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    client.validate().then(v => {
      if (v.role !== "admin") {
        // Real enforcement is server-side regardless — this just keeps a
        // member from landing on a page that will 403 on every request.
        router.push("/");
        return;
      }
      setOrg({
        id: v.org_id,
        name: v.org_name,
        plan: v.plan,
        activation_state: v.activation_state,
        domain_join: v.domain_join,
        primary_domain: v.primary_domain,
      });
      setLoading(false);
    }).catch(() => {
      router.push("/");
    });
  }, [router]);

  if (loading || !org) {
    return <LoadingState label="Loading team…" className="h-full" />;
  }

  return (
    <div className="p-8 max-w-7xl mx-auto h-full flex flex-col text-white">
      <OrgManagementPanel org={org} onOrgUpdate={setOrg} />
    </div>
  );
}
```

- [ ] **Step 2: Add the nav item**

In `frontend/src/app/(dashboard)/layout.tsx`, replace:

```tsx
import { LayoutDashboard, Network, Settings, Server, Cpu, Link2, LogOut, Shield, Receipt } from "lucide-react";
```

with:

```tsx
import { LayoutDashboard, Network, Settings, Server, Cpu, Link2, LogOut, Shield, Receipt, Users } from "lucide-react";
```

Replace:

```tsx
  const [isSuperAdmin, setIsSuperAdmin] = useState<boolean>(false);
  const [plan, setPlan] = useState<string | null>(null);
```

with:

```tsx
  const [isSuperAdmin, setIsSuperAdmin] = useState<boolean>(false);
  const [isOrgAdmin, setIsOrgAdmin] = useState<boolean>(false);
  const [plan, setPlan] = useState<string | null>(null);
```

Replace:

```tsx
      client.getUsage().then(usage => {
        setIsSuperAdmin(!!usage.is_super_admin);
        setPlan(usage.plan);
```

with:

```tsx
      client.getUsage().then(usage => {
        setIsSuperAdmin(!!usage.is_super_admin);
        setPlan(usage.plan);
        client.validate().then(v => setIsOrgAdmin(v.role === "admin")).catch(() => {});
```

Replace:

```tsx
  if (isSuperAdmin) {
    navItems.push({ name: "Admin", href: "/admin", icon: Shield });
  }
```

with:

```tsx
  if (isOrgAdmin || isSuperAdmin) {
    navItems.push({ name: "Team", href: "/team", icon: Users });
  }

  if (isSuperAdmin) {
    navItems.push({ name: "Admin", href: "/admin", icon: Shield });
  }
```

- [ ] **Step 3: Type-check**

Run (from `frontend/`): `npm run build`
Expected: succeeds, no type errors.

- [ ] **Step 4: Commit**

```bash
git add "frontend/src/app/(dashboard)/team/page.tsx" "frontend/src/app/(dashboard)/layout.tsx"
git commit -m "$(cat <<'EOF'
feat(frontend): add self-service /team page for org-scoped admins

Renders the same OrgManagementPanel as the super-admin org page, but
seeded from /auth/validate (own org only, no org picker) instead of
the super-admin-only listAdminOrgs. Nav shows "Team" whenever the
caller is an org-scoped admin or a super-admin.
EOF
)"
```

---

### Task 9: Manual end-to-end verification

**Files:** none — this task runs the app, it doesn't change it.

**Interfaces:** none.

- [ ] **Step 1: Start the stack**

Use the `run` skill (per this repo's project-skill precedence — check for a project-specific launch skill first) to bring up Postgres/NATS/MinIO (`make run-local`), the control plane (`kiwid`), and the frontend dev server, per the commands in the root `CLAUDE.md` §4.

- [ ] **Step 2: Create two orgs and set up cross-org test accounts**

Using the bootstrap `KIWI_SERVER_TOKEN` against `/admin/orgs`, create two orgs (`Org Alpha`, `Org Beta`). In each, create one `admin` user and one `member` user via `POST /admin/orgs/{orgID}/users`, then mint each a session-equivalent API key via `POST /admin/orgs/{orgID}/users/{userID}/keys` (label it, e.g., `"manual-test"`) so you can log the frontend in as each by placing the plaintext key in `localStorage.kiwi_token` (the same key `frontend/src/lib/auth.ts` reads).

- [ ] **Step 3: Verify the org-admin self-service path**

Logged in as Org Alpha's admin:
- The sidebar shows a "Team" entry; the sidebar's "Admin" entry does not appear.
- `/team` loads Org Alpha's own panel (no org picker). Users tab: create a user, confirm it appears; expand a user's "Keys" row, generate a key, confirm the plaintext banner and copy button work, then revoke it and confirm it disappears. Usage tab: confirm it loads without a 403 (data may be empty on a fresh org — that's fine, the point is the request succeeds). Provider Config tab: save a config, confirm it persists on reload. Audit Logs tab: confirm the user-creation and provider-config actions appear. Access tab: toggle domain-join on/off, confirm it persists on reload; if a join request exists (see Step 4), approve and deny each show up correctly.
- Rename the org via the pencil icon in the header; confirm the new name persists after a reload and appears correctly wherever the org name is displayed elsewhere in the dashboard (e.g. Settings page).
- Manually navigate to `/admin` and to `/admin/orgs/{Org-Beta-ID}` — both must redirect to `/`.

- [ ] **Step 4: Verify the member is still locked out**

Logged in as Org Alpha's member:
- The sidebar shows neither "Team" nor "Admin".
- Manually navigating to `/team` redirects to `/`.

- [ ] **Step 5: Verify cross-org isolation**

Logged in as Org Beta's admin, attempt (via a direct authenticated request, e.g. curl with Org Beta's admin API key) each of: `GET /admin/orgs/{Org-Alpha-ID}/users`, `GET /admin/orgs/{Org-Alpha-ID}/audit`, `GET /admin/orgs/{Org-Alpha-ID}/model_usage`, `PUT /admin/orgs/{Org-Alpha-ID}/domain_join`, `PUT /admin/orgs/{Org-Alpha-ID}/name`. Every one must return `403`.

- [ ] **Step 6: Verify the super-admin path is unaffected**

Set `KIWI_SUPER_ADMIN_EMAILS` to include a test super-admin's email (or use the bootstrap token), log in, confirm `/admin` still lists both orgs and every existing action (create org, change plan, grant minutes, activate/suspend) still works, and that `/admin/orgs/{orgID}` for either org now also shows the new Access tab and rename control.

- [ ] **Step 7: Report results**

No commit for this task. If any check in Steps 3–6 fails, open a new task (not a silent fix) describing the gap before touching code — this plan's tasks are meant to be reviewed as landed, not amended after the fact.
