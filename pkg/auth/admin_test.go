package auth

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
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
