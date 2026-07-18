package auth

import (
	"bytes"
	"encoding/json"
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
