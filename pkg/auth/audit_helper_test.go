package auth

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/ibreakthecloud/kiwi/pkg/audit"
	"gorm.io/gorm"
)

func setupAuthAuditDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&audit.AuditLog{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func TestLogAuditEventExtractsClaims(t *testing.T) {
	db := setupAuthAuditDB(t)

	req := httptest.NewRequest("POST", "/tasks", nil)
	req.RemoteAddr = "10.0.0.1"
	ctx := ContextWithClaims(context.Background(), &UserClaims{
		UserID: "user-2", Email: "admin@test.com", OrgID: "org-alpha", Role: "admin",
	})
	req = req.WithContext(ctx)

	if err := LogAuditEvent(db, req, "REVOKE", "API_KEY", "key-200", "Revoked API key"); err != nil {
		t.Fatalf("LogAuditEvent error: %v", err)
	}

	logs, err := audit.GetOrgAuditLogs(db, "org-alpha")
	if err != nil {
		t.Fatalf("GetOrgAuditLogs error: %v", err)
	}
	if len(logs) != 1 {
		t.Fatalf("expected 1 log, got %d", len(logs))
	}
	got := logs[0]
	if got.UserID != "user-2" || got.UserEmail != "admin@test.com" || got.OrgID != "org-alpha" || got.ClientIP != "10.0.0.1" || got.Action != "REVOKE" {
		t.Errorf("claims not extracted into audit record: %+v", got)
	}
}

func TestLogAuditEventNilRequestDefaultsSystem(t *testing.T) {
	db := setupAuthAuditDB(t)
	if err := LogAuditEvent(db, nil, "EXECUTE", "TASK", "t1", "system op"); err != nil {
		t.Fatalf("LogAuditEvent nil req error: %v", err)
	}
	logs, _ := audit.GetOrgAuditLogs(db, "system")
	if len(logs) != 1 {
		t.Fatalf("expected 1 system log, got %d", len(logs))
	}
}
