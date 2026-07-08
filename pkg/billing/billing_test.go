package billing

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// TaskStateMock mirrors orchestrator.TaskState for testing DB aggregations
// without circular dependencies.
type TaskStateMock struct {
	ID        string    `gorm:"primaryKey"`
	OrgID     string    `gorm:"index"`
	UserID    string    `gorm:"index"`
	UserEmail string    `gorm:"user_email"`
	Status    string    `gorm:"index"`
	Cost      float64   `gorm:"cost"`
	CreatedAt time.Time `gorm:"created_at"`
}

func (TaskStateMock) TableName() string { return "task_states" }

func setupBillingTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	if err := db.AutoMigrate(&TaskStateMock{}); err != nil {
		t.Fatalf("failed to migrate mock DB: %v", err)
	}
	return db
}

func TestGetOrgAndUserUsage(t *testing.T) {
	db := setupBillingTestDB(t)

	now := time.Now()
	orgID := "org-alpha"

	// Create test task data across two users in org-alpha
	tasks := []TaskStateMock{
		{ID: "t1", OrgID: orgID, UserID: "u1", UserEmail: "u1@test.com", Status: "SUCCESS", Cost: 1.50, CreatedAt: now},
		{ID: "t2", OrgID: orgID, UserID: "u1", UserEmail: "u1@test.com", Status: "SUCCESS", Cost: 2.00, CreatedAt: now},
		{ID: "t3", OrgID: orgID, UserID: "u2", UserEmail: "u2@test.com", Status: "FAILED", Cost: 0.50, CreatedAt: now},
		{ID: "t4", OrgID: "org-beta", UserID: "u3", UserEmail: "u3@test.com", Status: "SUCCESS", Cost: 5.00, CreatedAt: now}, // separate org
	}
	for _, tk := range tasks {
		db.Create(&tk)
	}

	from := now.Add(-1 * time.Hour)
	to := now.Add(1 * time.Hour)

	// 1. Test Org-level usage statistics
	orgUsage, err := GetOrgUsage(db, orgID, from, to)
	if err != nil {
		t.Fatalf("GetOrgUsage error: %v", err)
	}
	if orgUsage.OrgID != orgID {
		t.Errorf("expected org ID %q, got %q", orgID, orgUsage.OrgID)
	}
	if orgUsage.TotalCost != 4.00 {
		t.Errorf("expected total cost $4.00, got %.2f", orgUsage.TotalCost)
	}
	if orgUsage.TaskCount != 3 {
		t.Errorf("expected task count 3, got %d", orgUsage.TaskCount)
	}
	if orgUsage.SuccessCount != 2 {
		t.Errorf("expected successes 2, got %d", orgUsage.SuccessCount)
	}
	if orgUsage.FailedCount != 1 {
		t.Errorf("expected failures 1, got %d", orgUsage.FailedCount)
	}

	// Verify TopUsers sorting
	if len(orgUsage.TopUsers) != 2 {
		t.Fatalf("expected 2 users in stats, got %d", len(orgUsage.TopUsers))
	}
	// u1 should be first because $3.50 > $0.50
	if orgUsage.TopUsers[0].UserID != "u1" || orgUsage.TopUsers[0].TotalCost != 3.50 {
		t.Errorf("expected u1 as top spender with $3.50, got: %+v", orgUsage.TopUsers[0])
	}
	if orgUsage.TopUsers[1].UserID != "u2" || orgUsage.TopUsers[1].TotalCost != 0.50 {
		t.Errorf("expected u2 as second spender with $0.50, got: %+v", orgUsage.TopUsers[1])
	}

	// 2. Test User-level usage statistics
	userUsage, err := GetUserUsage(db, "u1", from, to)
	if err != nil {
		t.Fatalf("GetUserUsage error: %v", err)
	}
	if userUsage.UserID != "u1" {
		t.Errorf("expected user ID 'u1', got %q", userUsage.UserID)
	}
	if userUsage.TotalCost != 3.50 {
		t.Errorf("expected user u1 total cost $3.50, got %.2f", userUsage.TotalCost)
	}
	if userUsage.TaskCount != 2 {
		t.Errorf("expected user u1 task count 2, got %d", userUsage.TaskCount)
	}
}

func TestParseDateParams(t *testing.T) {
	// Case 1: Defaults (no params)
	req1 := httptest.NewRequest("GET", "/usage", nil)
	from1, to1, err := ParseDateParams(req1)
	if err != nil {
		t.Fatalf("ParseDateParams error: %v", err)
	}
	now := time.Now()
	if from1.Year() != now.Year() || from1.Month() != now.Month() || from1.Day() != 1 {
		t.Errorf("expected default 'from' to be start of month, got %v", from1)
	}
	if to1.After(now) {
		t.Errorf("expected default 'to' to be roughly now, got %v", to1)
	}

	// Case 2: Unix timestamp params
	req2 := httptest.NewRequest("GET", "/usage?from=1700000000&to=1710000000", nil)
	from2, to2, err := ParseDateParams(req2)
	if err != nil {
		t.Fatalf("ParseDateParams error: %v", err)
	}
	if from2.Unix() != 1700000000 {
		t.Errorf("expected from unix 1700000000, got %d", from2.Unix())
	}
	if to2.Unix() != 1710000000 {
		t.Errorf("expected to unix 1710000000, got %d", to2.Unix())
	}

	// Case 3: RFC3339 string params
	req3 := httptest.NewRequest("GET", "/usage?from=2026-07-01T00:00:00Z&to=2026-07-08T00:00:00Z", nil)
	from3, to3, err := ParseDateParams(req3)
	if err != nil {
		t.Fatalf("ParseDateParams error: %v", err)
	}
	if from3.Format(time.RFC3339) != "2026-07-01T00:00:00Z" {
		t.Errorf("expected from RFC3339 string match, got %s", from3.Format(time.RFC3339))
	}
	if to3.Format(time.RFC3339) != "2026-07-08T00:00:00Z" {
		t.Errorf("expected to RFC3339 string match, got %s", to3.Format(time.RFC3339))
	}
}
