package audit

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/ibreakthecloud/kiwi/pkg/auth"
	"gorm.io/gorm"
)

func setupAuditTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	if err := db.AutoMigrate(&AuditLog{}); err != nil {
		t.Fatalf("failed to migrate audit DB: %v", err)
	}
	return db
}

func TestLogEventAndRetrieve(t *testing.T) {
	db := setupAuditTestDB(t)

	// 1. Log direct event
	err := LogEventDirect(db, "org-alpha", "user-1", "user@test.com", "CREATE", "TASK", "task-100", "Created test task", "127.0.0.1")
	if err != nil {
		t.Fatalf("LogEventDirect error: %v", err)
	}

	// 2. Log request event with context claims
	req := httptest.NewRequest("POST", "/tasks", nil)
	claims := &auth.UserClaims{
		UserID:    "user-2",
		Email:     "admin@test.com",
		UserEmail: "admin@test.com",
		OrgID:     "org-alpha",
		Role:      "admin",
	}
	ctx := auth.ContextWithClaims(context.Background(), claims)
	req = req.WithContext(ctx)
	req.RemoteAddr = "10.0.0.1"

	err = LogEvent(db, req, "REVOKE", "API_KEY", "key-200", "Revoked API key for user-2")
	if err != nil {
		t.Fatalf("LogEvent error: %v", err)
	}

	// 3. Log event from foreign org
	err = LogEventDirect(db, "org-beta", "user-3", "beta@test.com", "CREATE", "TASK", "task-300", "Foreign task", "127.0.0.1")
	if err != nil {
		t.Fatalf("LogEventDirect org-beta error: %v", err)
	}

	// 4. Retrieve org-alpha logs and verify
	logs, err := GetOrgAuditLogs(db, "org-alpha")
	if err != nil {
		t.Fatalf("GetOrgAuditLogs error: %v", err)
	}

	if len(logs) != 2 {
		t.Errorf("expected 2 audit logs for org-alpha, got %d", len(logs))
	}

	// Verify order is DESC (newest first)
	if logs[0].Action != "REVOKE" || logs[0].ResourceID != "key-200" || logs[0].ClientIP != "10.0.0.1" {
		t.Errorf("unexpected first log record details: %+v", logs[0])
	}
	if logs[1].Action != "CREATE" || logs[1].ResourceID != "task-100" || logs[1].UserEmail != "user@test.com" {
		t.Errorf("unexpected second log record details: %+v", logs[1])
	}
}
