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
