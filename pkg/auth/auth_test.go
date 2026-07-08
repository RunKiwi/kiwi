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
