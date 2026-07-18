package orchestrator

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"github.com/ibreakthecloud/kiwi/pkg/auth"
	"github.com/ibreakthecloud/kiwi/pkg/store"
)

func TestHandleSetCredential(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&store.Credential{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	st := store.NewPostgresStore(db)
	s := &Server{db: db, storage: st}

	t.Run("success", func(t *testing.T) {
		body, _ := json.Marshal(map[string]string{
			"name":  "MY_KEY",
			"kind":  "llm",
			"value": "secret",
		})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/credentials", bytes.NewReader(body))
		ctx := auth.ContextWithClaims(req.Context(), &auth.UserClaims{OrgID: "org-123"})
		req = req.WithContext(ctx)

		rr := httptest.NewRecorder()
		s.handleSetCredential(rr, req)

		if rr.Code != http.StatusNoContent {
			t.Errorf("expected 204, got %d", rr.Code)
		}

		// Verify stored and encrypted
		var cred store.Credential
		if err := db.First(&cred, "org_id = ? AND name = ?", "org-123", "MY_KEY").Error; err != nil {
			t.Fatalf("credential not found: %v", err)
		}
		if string(cred.EncryptedValue) == "" || string(cred.EncryptedValue) == "secret" {
			t.Errorf("value not properly encrypted: %v", string(cred.EncryptedValue))
		}
	})

	t.Run("unauthorized", func(t *testing.T) {
		body, _ := json.Marshal(map[string]string{"name": "MY_KEY", "value": "secret"})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/credentials", bytes.NewReader(body))

		rr := httptest.NewRecorder()
		s.handleSetCredential(rr, req)

		if rr.Code != http.StatusUnauthorized {
			t.Errorf("expected 401, got %d", rr.Code)
		}
	})

	t.Run("missing value", func(t *testing.T) {
		body, _ := json.Marshal(map[string]string{"name": "MY_KEY", "value": ""})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/credentials", bytes.NewReader(body))
		ctx := auth.ContextWithClaims(req.Context(), &auth.UserClaims{OrgID: "org-123"})
		req = req.WithContext(ctx)

		rr := httptest.NewRecorder()
		s.handleSetCredential(rr, req)

		if rr.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", rr.Code)
		}
	})

	t.Run("invalid name format", func(t *testing.T) {
		body, _ := json.Marshal(map[string]string{"name": "invalid-name", "value": "secret"})
		req := httptest.NewRequest(http.MethodPost, "/api/v1/credentials", bytes.NewReader(body))
		ctx := auth.ContextWithClaims(req.Context(), &auth.UserClaims{OrgID: "org-123"})
		req = req.WithContext(ctx)

		rr := httptest.NewRecorder()
		s.handleSetCredential(rr, req)

		if rr.Code != http.StatusBadRequest {
			t.Errorf("expected 400, got %d", rr.Code)
		}
	})
}
