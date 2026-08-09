// SPDX-License-Identifier: LicenseRef-Kiwi-BSL-1.1
// Copyright (c) 2026 RunKiwi. Licensed under the Business Source License 1.1.
// See ee/LICENSE. This is Control Plane code and is NOT Apache-2.0.

package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/ibreakthecloud/kiwi/ee/audit"
	"github.com/ibreakthecloud/kiwi/pkg/store"
)

func TestAdminRouter_Auth(t *testing.T) {
	db := setupTestDB(t)
	mux := http.NewServeMux()
	AdminRouter(db, mux)

	// Test without token
	req := httptest.NewRequest(http.MethodPost, "/admin/orgs", bytes.NewReader([]byte(`{"name":"test-org"}`)))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Errorf("Expected 403 without admin token, got %d", w.Code)
	}

	// Test with wrong token
	os.Setenv("KIWI_SERVER_TOKEN", "super-secret")
	defer os.Unsetenv("KIWI_SERVER_TOKEN")

	req = httptest.NewRequest(http.MethodPost, "/admin/orgs", bytes.NewReader([]byte(`{"name":"test-org"}`)))
	req.Header.Set("Authorization", "Bearer wrong-token")
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusForbidden {
		t.Errorf("Expected 403 with wrong token, got %d", w.Code)
	}

	// Test with correct token
	req = httptest.NewRequest(http.MethodPost, "/admin/orgs", bytes.NewReader([]byte(`{"name":"test-org"}`)))
	req.Header.Set("Authorization", "Bearer super-secret")
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Errorf("Expected 201 with correct token, got %d. Body: %s", w.Code, w.Body.String())
	}

	// Verify org was created
	var org struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := json.NewDecoder(w.Body).Decode(&org); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}
	if org.Name != "test-org" {
		t.Errorf("Expected org name 'test-org', got %s", org.Name)
	}
}

func TestAdminRouter_ClaimsAuth(t *testing.T) {
	db := setupTestDB(t)
	mux := http.NewServeMux()
	AdminRouter(db, mux)

	tests := []struct {
		name         string
		claims       *UserClaims
		setupEnv     func()
		expectedCode int
	}{
		{
			name: "org admin should be rejected",
			claims: &UserClaims{
				Role:  "admin",
				Email: "org-admin@example.com",
			},
			setupEnv:     func() {},
			expectedCode: http.StatusForbidden,
		},
		{
			name: "super admin should be authorized",
			claims: &UserClaims{
				Role:  "member",
				Email: "SUPER-ADMIN@example.com",
			},
			setupEnv: func() {
				t.Setenv("KIWI_SUPER_ADMIN_EMAILS", "other@foo.com, super-admin@example.com ")
			},
			expectedCode: http.StatusCreated,
		},
		{
			name: "system user should be authorized",
			claims: &UserClaims{
				UserID: "system",
			},
			setupEnv:     func() {},
			expectedCode: http.StatusCreated,
		},
	}

	for i, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tc.setupEnv()
			orgName := fmt.Sprintf("test-org-claims-%d", i)
			req := httptest.NewRequest(http.MethodPost, "/admin/orgs", bytes.NewReader([]byte(`{"name":"`+orgName+`"}`)))

			// Inject claims into context
			req = req.WithContext(ContextWithClaims(req.Context(), tc.claims))

			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)

			if w.Code != tc.expectedCode {
				t.Errorf("Expected %d, got %d", tc.expectedCode, w.Code)
			}
		})
	}
}

func TestAdminAPIEndpoints(t *testing.T) {
	db := setupTestDB(t)
	mux := http.NewServeMux()
	AdminRouter(db, mux)

	// create an org
	org := Organization{ID: "test-org-1", Name: "Test Org 1", Plan: "free"}
	db.Create(&org)
	db.Create(&OrgLimits{OrgID: org.ID, MaxAgentMinutesPerMonth: 100})

	claims := &UserClaims{UserID: "system"}
	ctx := ContextWithClaims(context.Background(), claims)

	// test stats
	reqStats := httptest.NewRequest(http.MethodGet, "/admin/stats", nil).WithContext(ctx)
	wStats := httptest.NewRecorder()
	mux.ServeHTTP(wStats, reqStats)
	if wStats.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", wStats.Code)
	}

	// test plan update
	bodyPlan := bytes.NewReader([]byte(`{"plan":"pro"}`))
	reqPlan := httptest.NewRequest(http.MethodPost, "/admin/orgs/test-org-1/plan", bodyPlan).WithContext(ctx)
	wPlan := httptest.NewRecorder()
	mux.ServeHTTP(wPlan, reqPlan)
	if wPlan.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", wPlan.Code)
	}
	var updatedOrg Organization
	db.First(&updatedOrg, "id = ?", "test-org-1")
	if updatedOrg.Plan != "pro" {
		t.Errorf("expected plan 'pro', got %s", updatedOrg.Plan)
	}

	// test grant
	bodyGrant := bytes.NewReader([]byte(`{"agent_minutes":500}`))
	reqGrant := httptest.NewRequest(http.MethodPost, "/admin/orgs/test-org-1/grant", bodyGrant).WithContext(ctx)
	wGrant := httptest.NewRecorder()
	mux.ServeHTTP(wGrant, reqGrant)
	if wGrant.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", wGrant.Code)
	}
	var limits OrgLimits
	db.First(&limits, "org_id = ?", "test-org-1")
	if limits.MaxAgentMinutesPerMonth != 600 {
		t.Errorf("expected 600 limits, got %f", limits.MaxAgentMinutesPerMonth)
	}
}

func TestAdminStats_ModelUsage(t *testing.T) {
	db := setupTestDB(t)
	if err := db.AutoMigrate(&store.Job{}, &store.QueuedTask{}); err != nil {
		t.Fatalf("migrate store models: %v", err)
	}
	mux := http.NewServeMux()
	AdminRouter(db, mux)

	// Two orgs, so the aggregation must be platform-wide, not scoped to one.
	db.Create(&Organization{ID: "org-a", Name: "Org A"})
	db.Create(&Organization{ID: "org-b", Name: "Org B"})

	// A BYOK job: planner spend on a claude model, real dollars.
	db.Create(&store.Job{
		ID: "job-1", OrgID: "org-a", UserID: "u1", Status: "SUCCEEDED",
		Inputs:  map[string]interface{}{"planner_model": "claude-sonnet-4-5"},
		Funding: store.FundingBYOK, PlannerCostUSD: 1.5, PlannerTokensIn: 1000, PlannerTokensOut: 500,
	})
	// A Kiwi-funded job on the same model: real cost still recorded, but it
	// must land in KiwiCostUSD, not be indistinguishable from billed spend.
	db.Create(&store.Job{
		ID: "job-2", OrgID: "org-b", UserID: "u2", Status: "SUCCEEDED",
		Inputs:  map[string]interface{}{"planner_model": "claude-sonnet-4-5"},
		Funding: store.FundingKiwi, PlannerCostUSD: 0.3, PlannerTokensIn: 200, PlannerTokensOut: 100,
	})
	// A worker task on a different provider entirely.
	db.Create(&store.QueuedTask{
		ID: "task-1", OrgID: "org-a", JobID: "job-1", Status: "SUCCEEDED",
		Spec:    map[string]interface{}{"model": "gpt-5"},
		Funding: store.FundingBYOK, CostUSD: 2.0, TokensIn: 3000, TokensOut: 1000,
	})

	claims := &UserClaims{UserID: "system"}
	ctx := ContextWithClaims(context.Background(), claims)
	req := httptest.NewRequest(http.MethodGet, "/admin/stats", nil).WithContext(ctx)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		ModelUsage    []AdminUsageRow `json:"model_usage"`
		ProviderUsage []AdminUsageRow `json:"provider_usage"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	byModel := map[string]AdminUsageRow{}
	for _, row := range resp.ModelUsage {
		byModel[row.Model] = row
	}

	sonnet, ok := byModel["claude-sonnet-4-5"]
	if !ok {
		t.Fatalf("expected claude-sonnet-4-5 in model_usage, got %+v", resp.ModelUsage)
	}
	if sonnet.Provider != "anthropic" {
		t.Errorf("expected provider anthropic, got %s", sonnet.Provider)
	}
	if sonnet.TaskCount != 2 {
		t.Errorf("expected 2 rows aggregated (both orgs), got %d", sonnet.TaskCount)
	}
	if got, want := sonnet.CostUSD, 1.8; got != want {
		t.Errorf("expected total cost %.2f (byok + kiwi), got %.2f", want, got)
	}
	if got, want := sonnet.KiwiCostUSD, 0.3; got != want {
		t.Errorf("expected kiwi-only cost %.2f, got %.2f", want, got)
	}

	gpt, ok := byModel["gpt-5"]
	if !ok {
		t.Fatalf("expected gpt-5 in model_usage, got %+v", resp.ModelUsage)
	}
	if gpt.Provider != "openai" {
		t.Errorf("expected provider openai, got %s", gpt.Provider)
	}
	if gpt.KiwiCostUSD != 0 {
		t.Errorf("expected no kiwi cost for a byok task, got %.2f", gpt.KiwiCostUSD)
	}

	byProvider := map[string]AdminUsageRow{}
	for _, row := range resp.ProviderUsage {
		byProvider[row.Provider] = row
	}
	if got, want := byProvider["anthropic"].CostUSD, 1.8; got != want {
		t.Errorf("expected anthropic provider total %.2f, got %.2f", want, got)
	}
	if _, ok := byProvider["openai"]; !ok {
		t.Errorf("expected openai in provider_usage, got %+v", resp.ProviderUsage)
	}
}

func TestAdminOrgModelUsage(t *testing.T) {
	db := setupTestDB(t)
	if err := db.AutoMigrate(&store.Job{}, &store.QueuedTask{}); err != nil {
		t.Fatalf("migrate store models: %v", err)
	}
	mux := http.NewServeMux()
	AdminRouter(db, mux)

	db.Create(&Organization{ID: "org-a", Name: "Org A"})
	db.Create(&Organization{ID: "org-b", Name: "Org B"})
	db.Create(&User{ID: "u1", OrgID: "org-a", Email: "alice@example.com", Name: "Alice"})
	db.Create(&User{ID: "u2", OrgID: "org-a", Email: "bob@example.com", Name: "Bob"})

	// Alice's job: one succeeded, one failed task.
	db.Create(&store.Job{
		ID: "job-1", OrgID: "org-a", UserID: "u1", Status: "SUCCEEDED",
		Inputs:  map[string]interface{}{"planner_model": "claude-sonnet-4-5"},
		Funding: store.FundingBYOK, PlannerCostUSD: 1.0,
	})
	db.Create(&store.QueuedTask{
		ID: "task-1", OrgID: "org-a", JobID: "job-1", Status: "SUCCEEDED",
		Spec: map[string]interface{}{"model": "claude-sonnet-4-5"}, Funding: store.FundingBYOK, CostUSD: 0.5,
	})
	db.Create(&store.QueuedTask{
		ID: "task-2", OrgID: "org-a", JobID: "job-1", Status: "FAILED",
		Spec: map[string]interface{}{"model": "claude-sonnet-4-5"}, Funding: store.FundingBYOK, CostUSD: 0.2,
	})

	// Bob's job in the same org, Kiwi-funded.
	db.Create(&store.Job{
		ID: "job-2", OrgID: "org-a", UserID: "u2", Status: "SUCCEEDED",
		Inputs: map[string]interface{}{"planner_model": "gpt-5"}, Funding: store.FundingKiwi, PlannerCostUSD: 0.4,
	})
	db.Create(&store.QueuedTask{
		ID: "task-3", OrgID: "org-a", JobID: "job-2", Status: "SUCCEEDED",
		Spec: map[string]interface{}{"model": "gpt-5"}, Funding: store.FundingKiwi, CostUSD: 0.6,
	})

	// A job in the OTHER org — must not leak into org-a's numbers.
	db.Create(&store.Job{
		ID: "job-3", OrgID: "org-b", UserID: "u3", Status: "SUCCEEDED",
		Inputs: map[string]interface{}{"planner_model": "claude-sonnet-4-5"}, Funding: store.FundingBYOK, PlannerCostUSD: 99,
	})
	db.Create(&store.QueuedTask{
		ID: "task-4", OrgID: "org-b", JobID: "job-3", Status: "SUCCEEDED",
		Spec: map[string]interface{}{"model": "claude-sonnet-4-5"}, Funding: store.FundingBYOK, CostUSD: 99,
	})

	claims := &UserClaims{UserID: "system"}
	ctx := ContextWithClaims(context.Background(), claims)
	req := httptest.NewRequest(http.MethodGet, "/admin/orgs/org-a/model_usage", nil).WithContext(ctx)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		ModelUsage    []AdminUsageRow     `json:"model_usage"`
		ProviderUsage []AdminUsageRow     `json:"provider_usage"`
		TasksByStatus map[string]int64    `json:"tasks_by_status"`
		PerUser       []AdminUserUsageRow `json:"per_user"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	// Scoping: org-b's $99 job must not appear anywhere in org-a's response.
	for _, row := range resp.ModelUsage {
		if row.CostUSD >= 99 {
			t.Fatalf("org-b's cost leaked into org-a's model_usage: %+v", row)
		}
	}

	if got, want := resp.TasksByStatus["SUCCEEDED"], int64(2); got != want {
		t.Errorf("expected 2 succeeded tasks, got %d", got)
	}
	if got, want := resp.TasksByStatus["FAILED"], int64(1); got != want {
		t.Errorf("expected 1 failed task, got %d", got)
	}

	byUser := map[string]AdminUserUsageRow{}
	for _, row := range resp.PerUser {
		byUser[row.UserID] = row
	}

	alice, ok := byUser["u1"]
	if !ok {
		t.Fatalf("expected u1 (alice) in per_user, got %+v", resp.PerUser)
	}
	if alice.Email != "alice@example.com" {
		t.Errorf("expected alice's email resolved, got %q", alice.Email)
	}
	if alice.TaskCount != 2 || alice.Succeeded != 1 || alice.Failed != 1 {
		t.Errorf("expected alice: 2 tasks (1 succeeded, 1 failed), got %+v", alice)
	}
	if got, want := alice.CostUSD, 1.7; got != want { // 1.0 planner + 0.5 + 0.2 worker
		t.Errorf("expected alice cost %.2f, got %.2f", want, got)
	}
	if alice.KiwiCostUSD != 0 {
		t.Errorf("expected alice's cost to be entirely byok, got kiwi=%.2f", alice.KiwiCostUSD)
	}

	bob, ok := byUser["u2"]
	if !ok {
		t.Fatalf("expected u2 (bob) in per_user, got %+v", resp.PerUser)
	}
	if bob.TaskCount != 1 || bob.Succeeded != 1 {
		t.Errorf("expected bob: 1 succeeded task, got %+v", bob)
	}
	if got, want := bob.KiwiCostUSD, 1.0; got != want { // 0.4 planner + 0.6 worker, all kiwi-funded
		t.Errorf("expected bob's kiwi cost %.2f, got %.2f", want, got)
	}
}

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

		// pathFor, when set, is called once per assertion (own-org, cross-org,
		// member) to create a fresh resource and return the path to act on it,
		// overriding path. Used for routes that consume their target on success
		// (revoke, approve, deny) so one assertion's action can never affect
		// another's expected outcome.
		pathFor func(t *testing.T) string
	}

	cases := []routeCase{
		{name: "users list", method: http.MethodGet, path: "/admin/orgs/org-a/users", wantOwnStatus: http.StatusOK},
		{name: "keys list", method: http.MethodGet, path: "/admin/orgs/org-a/users/user-a-1/keys", wantOwnStatus: http.StatusOK},
		{name: "keys revoke", method: http.MethodDelete, wantOwnStatus: http.StatusNoContent, pathFor: func(t *testing.T) string {
			_, key, err := GenerateAPIKey(userA.ID, "revoke-test", nil)
			if err != nil {
				t.Fatalf("generate key: %v", err)
			}
			if err := db.Create(key).Error; err != nil {
				t.Fatalf("save key: %v", err)
			}
			return "/admin/orgs/org-a/users/user-a-1/keys/" + key.ID
		}},
		{name: "audit", method: http.MethodGet, path: "/admin/orgs/org-a/audit", wantOwnStatus: http.StatusOK},
		{name: "usage", method: http.MethodGet, path: "/admin/orgs/org-a/usage", wantOwnStatus: -1},
		{name: "model_usage", method: http.MethodGet, path: "/admin/orgs/org-a/model_usage", wantOwnStatus: -1},
		{name: "provider get", method: http.MethodGet, path: "/admin/orgs/org-a/provider", wantOwnStatus: http.StatusOK},
		{name: "join_requests list", method: http.MethodGet, path: "/admin/orgs/org-a/join_requests", wantOwnStatus: http.StatusOK},
		{name: "join_requests approve", method: http.MethodPost, wantOwnStatus: http.StatusOK, pathFor: func(t *testing.T) string {
			id, err := generateHexID(4)
			if err != nil {
				t.Fatalf("generate join request id: %v", err)
			}
			jr := OrgJoinRequest{ID: "req-approve-" + id, OrgID: "org-a", UserEmail: "joiner-" + id + "@example.com", Status: "pending"}
			if err := db.Create(&jr).Error; err != nil {
				t.Fatalf("create join request: %v", err)
			}
			return "/admin/orgs/org-a/join_requests/" + jr.ID + "/approve"
		}},
		{name: "join_requests deny", method: http.MethodPost, wantOwnStatus: http.StatusOK, pathFor: func(t *testing.T) string {
			id, err := generateHexID(4)
			if err != nil {
				t.Fatalf("generate join request id: %v", err)
			}
			jr := OrgJoinRequest{ID: "req-deny-" + id, OrgID: "org-a", UserEmail: "joiner-" + id + "@example.com", Status: "pending"}
			if err := db.Create(&jr).Error; err != nil {
				t.Fatalf("create join request: %v", err)
			}
			return "/admin/orgs/org-a/join_requests/" + jr.ID + "/deny"
		}},
		{name: "domain_join", method: http.MethodPut, path: "/admin/orgs/org-a/domain_join", body: `{"domain_join":true}`, wantOwnStatus: http.StatusOK},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ownPath := tc.path
			if tc.pathFor != nil {
				ownPath = tc.pathFor(t)
			}
			reqOwn := httptest.NewRequest(tc.method, ownPath, bytes.NewReader([]byte(tc.body)))
			reqOwn = reqOwn.WithContext(ContextWithClaims(reqOwn.Context(), adminA))
			wOwn := httptest.NewRecorder()
			mux.ServeHTTP(wOwn, reqOwn)
			if wOwn.Code == http.StatusForbidden {
				t.Errorf("org-admin should access own org's %s, got 403: %s", tc.name, wOwn.Body.String())
			}
			if tc.wantOwnStatus != -1 && wOwn.Code != tc.wantOwnStatus {
				t.Errorf("expected %d for own-org %s, got %d: %s", tc.wantOwnStatus, tc.name, wOwn.Code, wOwn.Body.String())
			}

			crossPath := tc.path
			if tc.pathFor != nil {
				crossPath = tc.pathFor(t)
			}
			reqCross := httptest.NewRequest(tc.method, crossPath, bytes.NewReader([]byte(tc.body)))
			reqCross = reqCross.WithContext(ContextWithClaims(reqCross.Context(), adminB))
			wCross := httptest.NewRecorder()
			mux.ServeHTTP(wCross, reqCross)
			if wCross.Code != http.StatusForbidden {
				t.Errorf("cross-org admin should be rejected on %s, got %d", tc.name, wCross.Code)
			}

			memberPath := tc.path
			if tc.pathFor != nil {
				memberPath = tc.pathFor(t)
			}
			reqMember := httptest.NewRequest(tc.method, memberPath, bytes.NewReader([]byte(tc.body)))
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
