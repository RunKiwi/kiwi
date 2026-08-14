// SPDX-License-Identifier: LicenseRef-Kiwi-BSL-1.1
// Copyright (c) 2026 RunKiwi. Licensed under the Business Source License 1.1.
// See ee/LICENSE. This is Control Plane code and is NOT Apache-2.0.

package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func setupTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	if err := InitAuthDB(db); err != nil {
		t.Fatalf("failed to migrate auth DB: %v", err)
	}
	return db
}

func TestGenerateAndValidateAPIKey(t *testing.T) {
	db := setupTestDB(t)

	// Create test Org and User
	org := Organization{ID: "org1", Name: "Test Org", CreatedAt: time.Now()}
	if err := db.Create(&org).Error; err != nil {
		t.Fatalf("create org: %v", err)
	}

	user := User{ID: "user1", Email: "user@test.com", Name: "Test User", OrgID: "org1", Role: "member", CreatedAt: time.Now()}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}

	// 1. Generate standard API Key
	plaintext, apiKey, err := GenerateAPIKey(user.ID, "test-key", nil)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	if err := db.Create(apiKey).Error; err != nil {
		t.Fatalf("save key: %v", err)
	}

	// Test AuthFunc with valid token
	req := httptest.NewRequest("GET", "/tasks", nil)
	req.Header.Set("Authorization", "Bearer "+plaintext)

	claims, err := AuthFunc(db, req)
	if err != nil {
		t.Fatalf("auth valid token: %v", err)
	}
	if claims.UserID != user.ID || claims.OrgID != org.ID || claims.Role != "member" {
		t.Errorf("claims mismatch: %+v", claims)
	}

	// 2. Test Expired Token
	past := time.Now().Add(-1 * time.Hour)
	_, apiKeyExp, _ := GenerateAPIKey(user.ID, "expired-key", &past)
	db.Create(apiKeyExp)

	reqExp := httptest.NewRequest("GET", "/tasks", nil)
	reqExp.Header.Set("Authorization", "Bearer "+apiKeyExp.ID) // Wait, we need to pass plaintext!
	// Wait, we didn't save the plaintext of apiKeyExp. Let's re-generate properly.
	plaintextExp, apiKeyExp2, _ := GenerateAPIKey(user.ID, "expired-key-2", &past)
	db.Create(apiKeyExp2)
	reqExp.Header.Set("Authorization", "Bearer "+plaintextExp)

	_, err = AuthFunc(db, reqExp)
	if err == nil || err.Error() != "token has expired" {
		t.Errorf("expected expired error, got: %v", err)
	}

	// 3. Test Revoked Token
	plaintextRev, apiKeyRev, _ := GenerateAPIKey(user.ID, "revoked-key", nil)
	now := time.Now()
	apiKeyRev.RevokedAt = &now
	db.Create(apiKeyRev)

	reqRev := httptest.NewRequest("GET", "/tasks", nil)
	reqRev.Header.Set("Authorization", "Bearer "+plaintextRev)

	_, err = AuthFunc(db, reqRev)
	if err == nil || err.Error() != "token has been revoked" {
		t.Errorf("expected revoked error, got: %v", err)
	}
}

func TestBootstrapAdminToken(t *testing.T) {
	db := setupTestDB(t)
	t.Setenv("KIWI_SERVER_TOKEN", "super-admin-secret-999")

	req := httptest.NewRequest("GET", "/tasks", nil)
	req.Header.Set("Authorization", "Bearer super-admin-secret-999")

	claims, err := AuthFunc(db, req)
	if err != nil {
		t.Fatalf("auth admin token: %v", err)
	}
	if claims.UserID != "system" || claims.OrgID != "system" || claims.Role != "admin" {
		t.Errorf("expected system admin claims, got: %+v", claims)
	}
}

func TestAuthMiddleware(t *testing.T) {
	db := setupTestDB(t)

	// Create org, user, key
	org := Organization{ID: "org1", Name: "Test Org", CreatedAt: time.Now()}
	db.Create(&org)
	user := User{ID: "user1", Email: "user@test.com", Name: "Test User", OrgID: "org1", Role: "member", CreatedAt: time.Now()}
	db.Create(&user)
	plaintext, apiKey, _ := GenerateAPIKey(user.ID, "test-key", nil)
	db.Create(apiKey)

	handlerCalled := false
	var capturedClaims *UserClaims

	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
		capturedClaims = ClaimsFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	middleware := AuthMiddleware(db, testHandler)

	// 1. Request without auth header
	req1 := httptest.NewRequest("GET", "/tasks", nil)
	w1 := httptest.NewRecorder()
	middleware.ServeHTTP(w1, req1)
	if w1.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w1.Code)
	}

	// 2. Request with valid key
	req2 := httptest.NewRequest("GET", "/tasks", nil)
	req2.Header.Set("Authorization", "Bearer "+plaintext)
	w2 := httptest.NewRecorder()
	middleware.ServeHTTP(w2, req2)

	if w2.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w2.Code)
	}
	if !handlerCalled {
		t.Errorf("handler not called")
	}
	if capturedClaims == nil || capturedClaims.UserID != "user1" {
		t.Errorf("claims not injected correctly")
	}
}

// Activity tracking must never fire for API-key authentication UNLESS the
// key is labeled WebSessionAPIKeyLabel — that label, not "cookie vs.
// bearer", is the actual browser-vs-CLI signal (see recordDashboardActivity
// and TestAuthMiddleware_WebSessionAPIKeyRecordsDashboardActivity). This
// test uses a "cli-key"-labeled key and proves the negative case: keys
// long-lived and reused across CLI/SDK/daemon calls for weeks must not have
// that reuse misrepresented as "dashboard activity", and must not turn
// every CLI invocation and daemon poll into a write.
func TestAuthMiddleware_APIKeyDoesNotRecordDashboardActivity(t *testing.T) {
	db := setupTestDB(t)

	org := Organization{ID: "org-nodash", Name: "No-Dashboard Org"}
	db.Create(&org)
	user := User{ID: "user-nodash", Email: "nodash@test.com", Name: "No Dash User", OrgID: org.ID, Role: "member"}
	db.Create(&user)
	plaintext, apiKey, _ := GenerateAPIKey(user.ID, "cli-key", nil)
	db.Create(apiKey)

	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	middleware := AuthMiddleware(db, testHandler)

	req := httptest.NewRequest("GET", "/tasks", nil)
	req.Header.Set("Authorization", "Bearer "+plaintext)
	w := httptest.NewRecorder()
	middleware.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var reloaded User
	if err := db.First(&reloaded, "id = ?", user.ID).Error; err != nil {
		t.Fatalf("reload user: %v", err)
	}
	if reloaded.LastSeenAt != nil {
		t.Errorf("expected last_seen_at to stay nil for API-key auth, got %v", reloaded.LastSeenAt)
	}

	var sessions []DashboardSession
	if err := db.Where("user_id = ?", user.ID).Find(&sessions).Error; err != nil {
		t.Fatalf("query sessions: %v", err)
	}
	if len(sessions) != 0 {
		t.Errorf("expected 0 dashboard sessions for API-key auth, got %d", len(sessions))
	}
}

// The real SPA authenticates every request with a bearer token minted by
// the OAuth callback (GenerateAPIKey(user.ID, "Web Session", nil) in
// oauth.go) — never the session cookie. So a bearer-token request
// presenting a WebSessionAPIKeyLabel key IS real browser dashboard
// activity, and must be tracked exactly like the cookie-fallback path. This
// is the actual production-fix verification for the bug where
// dashboard_sessions stayed empty and last_seen_at stayed null despite the
// cookie-path tests passing, because real traffic never took the cookie
// branch. AuthMiddleware and AuthFunc have independent, duplicated
// branches, so both are covered here and in the AuthFunc test below.
func TestAuthMiddleware_WebSessionAPIKeyRecordsDashboardActivity(t *testing.T) {
	db := setupTestDB(t)

	org := Organization{ID: "org-websess-mw", Name: "Web Session MW Org"}
	db.Create(&org)
	user := User{ID: "user-websess-mw", Email: "websess-mw@test.com", Name: "Web Session MW User", OrgID: org.ID, Role: "member"}
	db.Create(&user)
	plaintext, apiKey, _ := GenerateAPIKey(user.ID, WebSessionAPIKeyLabel, nil)
	db.Create(apiKey)

	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	middleware := AuthMiddleware(db, testHandler)

	req := httptest.NewRequest("GET", "/tasks", nil)
	req.Header.Set("Authorization", "Bearer "+plaintext)
	w := httptest.NewRecorder()
	middleware.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var reloaded User
	if err := db.First(&reloaded, "id = ?", user.ID).Error; err != nil {
		t.Fatalf("reload user: %v", err)
	}
	if reloaded.LastSeenAt == nil {
		t.Errorf("expected last_seen_at to be set for Web Session bearer auth, got nil")
	}

	var sessions []DashboardSession
	if err := db.Where("user_id = ?", user.ID).Find(&sessions).Error; err != nil {
		t.Fatalf("query sessions: %v", err)
	}
	if len(sessions) != 1 {
		t.Errorf("expected 1 dashboard session for Web Session bearer auth, got %d", len(sessions))
	}
}

// AuthFunc counterpart to TestAuthMiddleware_WebSessionAPIKeyRecordsDashboardActivity
// — AuthFunc has its own independent bearer-token branch, so the fix must
// be verified there too, not just in AuthMiddleware.
func TestAuthFunc_WebSessionAPIKeyRecordsDashboardActivity(t *testing.T) {
	db := setupTestDB(t)

	org := Organization{ID: "org-websess-fn", Name: "Web Session Func Org"}
	db.Create(&org)
	user := User{ID: "user-websess-fn", Email: "websess-fn@test.com", Name: "Web Session Func User", OrgID: org.ID, Role: "member"}
	db.Create(&user)
	plaintext, apiKey, _ := GenerateAPIKey(user.ID, WebSessionAPIKeyLabel, nil)
	db.Create(apiKey)

	req := httptest.NewRequest("GET", "/tasks", nil)
	req.Header.Set("Authorization", "Bearer "+plaintext)

	claims, err := AuthFunc(db, req)
	if err != nil {
		t.Fatalf("AuthFunc returned error: %v", err)
	}
	if claims == nil || claims.UserID != user.ID {
		t.Fatalf("expected claims for %s, got %+v", user.ID, claims)
	}

	var reloaded User
	if err := db.First(&reloaded, "id = ?", user.ID).Error; err != nil {
		t.Fatalf("reload user: %v", err)
	}
	if reloaded.LastSeenAt == nil {
		t.Errorf("expected last_seen_at to be set for Web Session bearer auth, got nil")
	}

	var sessions []DashboardSession
	if err := db.Where("user_id = ?", user.ID).Find(&sessions).Error; err != nil {
		t.Fatalf("query sessions: %v", err)
	}
	if len(sessions) != 1 {
		t.Errorf("expected 1 dashboard session for Web Session bearer auth, got %d", len(sessions))
	}
}
