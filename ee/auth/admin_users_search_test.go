// SPDX-License-Identifier: LicenseRef-Kiwi-BSL-1.1
// Copyright (c) 2026 RunKiwi. Licensed under the Business Source License 1.1.
// See ee/LICENSE. This is Control Plane code and is NOT Apache-2.0.

package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestHandleAdminUsersSearch(t *testing.T) {
	db := setupTestDB(t)
	mux := http.NewServeMux()
	AdminRouter(db, mux)

	// Seed Orgs
	org1 := Organization{ID: "org-1", Name: "Acme Corp", CreatedAt: time.Now().Add(-10 * time.Hour)}
	org2 := Organization{ID: "org-2", Name: "Beta Inc", CreatedAt: time.Now().Add(-10 * time.Hour)}
	if err := db.Create(&org1).Error; err != nil {
		t.Fatalf("failed to seed org1: %v", err)
	}
	if err := db.Create(&org2).Error; err != nil {
		t.Fatalf("failed to seed org2: %v", err)
	}

	githubProvider := "github"
	googleProvider := "google"
	now := time.Now()

	users := []User{
		{
			ID:            "usr-1",
			Email:         "alice@acme.com",
			Name:          "Alice Smith",
			OrgID:         "org-1",
			Role:          "admin",
			OAuthProvider: &githubProvider,
			CreatedAt:     now.Add(-4 * time.Hour),
		},
		{
			ID:            "usr-2",
			Email:         "bob@acme.com",
			Name:          "Bob Jones",
			OrgID:         "org-1",
			Role:          "member",
			OAuthProvider: nil,
			CreatedAt:     now.Add(-3 * time.Hour),
		},
		{
			ID:            "usr-3",
			Email:         "carol@beta.com",
			Name:          "Carol Danvers",
			OrgID:         "org-2",
			Role:          "member",
			OAuthProvider: &googleProvider,
			CreatedAt:     now.Add(-2 * time.Hour),
		},
		{
			ID:            "usr-4",
			Email:         "dave@beta.com",
			Name:          "Dave Bowman",
			OrgID:         "org-2",
			Role:          "admin",
			OAuthProvider: &githubProvider,
			CreatedAt:     now.Add(-1 * time.Hour),
		},
	}

	for _, u := range users {
		if err := db.Create(&u).Error; err != nil {
			t.Fatalf("failed to seed user %s: %v", u.ID, err)
		}
	}

	sysCtx := ContextWithClaims(context.Background(), &UserClaims{UserID: "system"})

	t.Run("search by name substring case-insensitive", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/admin/users?search=ALICE", nil).WithContext(sysCtx)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}

		var resp struct {
			Users []AdminUserSearchRow `json:"users"`
			Total int64                `json:"total"`
		}
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatalf("failed to decode json: %v", err)
		}

		if resp.Total != 1 {
			t.Errorf("expected total 1, got %d", resp.Total)
		}
		if len(resp.Users) != 1 {
			t.Fatalf("expected 1 user, got %d", len(resp.Users))
		}

		u := resp.Users[0]
		if u.ID != "usr-1" || u.Email != "alice@acme.com" || u.Name != "Alice Smith" ||
			u.OrgID != "org-1" || u.OrgName != "Acme Corp" || u.Role != "admin" ||
			u.AuthProvider != "github" {
			t.Errorf("unexpected user row: %+v", u)
		}
	})

	t.Run("search by email domain substring", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/admin/users?search=acme.com", nil).WithContext(sysCtx)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
		}

		var resp struct {
			Users []AdminUserSearchRow `json:"users"`
			Total int64                `json:"total"`
		}
		if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
			t.Fatalf("failed to decode json: %v", err)
		}

		if resp.Total != 2 {
			t.Errorf("expected total 2, got %d", resp.Total)
		}
		if len(resp.Users) != 2 {
			t.Fatalf("expected 2 users, got %d", len(resp.Users))
		}
		// Ordered by created_at desc: usr-2 (now - 3h), usr-1 (now - 4h)
		if resp.Users[0].ID != "usr-2" || resp.Users[1].ID != "usr-1" {
			t.Errorf("unexpected order: %s, %s", resp.Users[0].ID, resp.Users[1].ID)
		}
		if resp.Users[0].AuthProvider != "" {
			t.Errorf("expected empty AuthProvider for usr-2, got %s", resp.Users[0].AuthProvider)
		}
		if resp.Users[1].AuthProvider != "github" {
			t.Errorf("expected github AuthProvider for usr-1, got %s", resp.Users[1].AuthProvider)
		}
	})

	t.Run("pagination limit and offset", func(t *testing.T) {
		// All 4 users, limit 2, offset 0 -> newest 2: usr-4, usr-3
		req1 := httptest.NewRequest(http.MethodGet, "/admin/users?limit=2&offset=0", nil).WithContext(sysCtx)
		w1 := httptest.NewRecorder()
		mux.ServeHTTP(w1, req1)

		if w1.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w1.Code, w1.Body.String())
		}

		var resp1 struct {
			Users []AdminUserSearchRow `json:"users"`
			Total int64                `json:"total"`
		}
		if err := json.NewDecoder(w1.Body).Decode(&resp1); err != nil {
			t.Fatalf("failed to decode json: %v", err)
		}

		if resp1.Total != 4 {
			t.Errorf("expected total 4, got %d", resp1.Total)
		}
		if len(resp1.Users) != 2 {
			t.Fatalf("expected 2 users, got %d", len(resp1.Users))
		}
		if resp1.Users[0].ID != "usr-4" || resp1.Users[1].ID != "usr-3" {
			t.Errorf("unexpected page 1: %s, %s", resp1.Users[0].ID, resp1.Users[1].ID)
		}

		// Offset 2 -> usr-2, usr-1
		req2 := httptest.NewRequest(http.MethodGet, "/admin/users?limit=2&offset=2", nil).WithContext(sysCtx)
		w2 := httptest.NewRecorder()
		mux.ServeHTTP(w2, req2)

		if w2.Code != http.StatusOK {
			t.Fatalf("expected 200, got %d: %s", w2.Code, w2.Body.String())
		}

		var resp2 struct {
			Users []AdminUserSearchRow `json:"users"`
			Total int64                `json:"total"`
		}
		if err := json.NewDecoder(w2.Body).Decode(&resp2); err != nil {
			t.Fatalf("failed to decode json: %v", err)
		}

		if resp2.Total != 4 {
			t.Errorf("expected total 4, got %d", resp2.Total)
		}
		if len(resp2.Users) != 2 {
			t.Fatalf("expected 2 users, got %d", len(resp2.Users))
		}
		if resp2.Users[0].ID != "usr-2" || resp2.Users[1].ID != "usr-1" {
			t.Errorf("unexpected page 2: %s, %s", resp2.Users[0].ID, resp2.Users[1].ID)
		}
	})

	t.Run("server token auth", func(t *testing.T) {
		t.Setenv("KIWI_SERVER_TOKEN", "test-secret-token")
		req := httptest.NewRequest(http.MethodGet, "/admin/users?limit=1", nil)
		req.Header.Set("Authorization", "Bearer test-secret-token")
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 with server token, got %d: %s", w.Code, w.Body.String())
		}
	})
}

func TestHandleAdminUsersSearchRequiresSuperAdmin(t *testing.T) {
	db := setupTestDB(t)
	mux := http.NewServeMux()
	AdminRouter(db, mux)

	t.Run("unauthenticated returns 403", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/admin/users", nil)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)

		if w.Code != http.StatusForbidden {
			t.Errorf("expected 403 without auth, got %d", w.Code)
		}
	})

	t.Run("non-admin org member returns 403", func(t *testing.T) {
		memberCtx := ContextWithClaims(context.Background(), &UserClaims{
			UserID: "usr-member",
			OrgID:  "org-1",
			Role:   "member",
			Email:  "member@example.com",
		})
		req := httptest.NewRequest(http.MethodGet, "/admin/users", nil).WithContext(memberCtx)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)

		if w.Code != http.StatusForbidden {
			t.Errorf("expected 403 for member claims, got %d", w.Code)
		}
	})

	t.Run("org-scoped admin returns 403", func(t *testing.T) {
		adminCtx := ContextWithClaims(context.Background(), &UserClaims{
			UserID: "usr-admin",
			OrgID:  "org-1",
			Role:   "admin",
			Email:  "admin@example.com",
		})
		req := httptest.NewRequest(http.MethodGet, "/admin/users", nil).WithContext(adminCtx)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)

		if w.Code != http.StatusForbidden {
			t.Errorf("expected 403 for org-scoped admin, got %d", w.Code)
		}
	})

	t.Run("super admin by email succeeds", func(t *testing.T) {
		t.Setenv("KIWI_SUPER_ADMIN_EMAILS", "superadmin@example.com")
		superAdminCtx := ContextWithClaims(context.Background(), &UserClaims{
			UserID: "usr-super",
			OrgID:  "org-1",
			Role:   "member",
			Email:  "superadmin@example.com",
		})
		req := httptest.NewRequest(http.MethodGet, "/admin/users", nil).WithContext(superAdminCtx)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("expected 200 for super admin email, got %d", w.Code)
		}
	})

	t.Run("method not allowed", func(t *testing.T) {
		sysCtx := ContextWithClaims(context.Background(), &UserClaims{UserID: "system"})
		req := httptest.NewRequest(http.MethodPost, "/admin/users", nil).WithContext(sysCtx)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)

		if w.Code != http.StatusMethodNotAllowed {
			t.Errorf("expected 405 for POST /admin/users, got %d", w.Code)
		}
	})
}
