package auth

import (
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// TaskStateMock mirrors orchestrator.TaskState for testing DB aggregations
// without creating a circular dependency.
type TaskStateMock struct {
	ID        string    `gorm:"primaryKey"`
	OrgID     string    `gorm:"index"`
	Status    string    `gorm:"index"`
	Cost      float64   `gorm:"cost"`
	CreatedAt time.Time `gorm:"created_at"`
}

func (TaskStateMock) TableName() string { return "task_states" }

func setupLimitsTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	if err := db.AutoMigrate(&Organization{}, &OrgLimits{}, &TaskStateMock{}); err != nil {
		t.Fatalf("failed to migrate auth DB: %v", err)
	}
	return db
}

func TestOrgLimitsDefaultsAndOverrides(t *testing.T) {
	db := setupLimitsTestDB(t)
	orgID := "org-test"

	// 1. Retrieve limits when none exist in DB (should return defaults)
	limits, err := GetOrgLimits(db, orgID)
	if err != nil {
		t.Fatalf("GetOrgLimits error: %v", err)
	}
	if limits.MaxConcurrentTasks != 5 {
		t.Errorf("expected default concurrency 5, got %d", limits.MaxConcurrentTasks)
	}
	if limits.MaxBudgetPerTask != 1.00 {
		t.Errorf("expected default budget per task 1.00, got %.2f", limits.MaxBudgetPerTask)
	}
	if limits.DockerImage != "golang:1.21-alpine" {
		t.Errorf("expected default docker image golang:1.21-alpine, got %q", limits.DockerImage)
	}

	// 2. Set custom limits in DB and verify override
	custom := OrgLimits{
		OrgID:              orgID,
		MaxConcurrentTasks: 2,
		MaxBudgetPerTask:   2.50,
		MaxBudgetPerMonth:  20.00,
		MaxSandboxDiskMB:   100,
		DockerImage:        "python:3.10-alpine",
		TaskTimeoutMinutes: 15,
	}
	if err := db.Create(&custom).Error; err != nil {
		t.Fatalf("create custom limits: %v", err)
	}

	limits, err = GetOrgLimits(db, orgID)
	if err != nil {
		t.Fatalf("GetOrgLimits custom error: %v", err)
	}
	if limits.MaxConcurrentTasks != 2 {
		t.Errorf("expected custom concurrency 2, got %d", limits.MaxConcurrentTasks)
	}
	if limits.MaxBudgetPerTask != 2.50 {
		t.Errorf("expected custom budget per task 2.50, got %.2f", limits.MaxBudgetPerTask)
	}
	if limits.DockerImage != "python:3.10-alpine" {
		t.Errorf("expected custom docker image python:3.10-alpine, got %q", limits.DockerImage)
	}
}

func TestOrgLimitsConcurrentLimitCheck(t *testing.T) {
	db := setupLimitsTestDB(t)
	orgID := "org-test"

	limits := &OrgLimits{
		OrgID:              orgID,
		MaxConcurrentTasks: 2,
	}
	db.Create(limits)

	// Case 1: No active tasks
	ok, err := limits.CheckConcurrentLimit(db)
	if err != nil {
		t.Fatalf("CheckConcurrentLimit error: %v", err)
	}
	if !ok {
		t.Errorf("expected concurrent limit check to pass with 0 tasks")
	}

	// Case 2: One active task (below limit of 2)
	db.Create(&TaskStateMock{ID: "task1", OrgID: orgID, Status: "RUNNING", CreatedAt: time.Now()})
	ok, err = limits.CheckConcurrentLimit(db)
	if err != nil {
		t.Fatalf("CheckConcurrentLimit error: %v", err)
	}
	if !ok {
		t.Errorf("expected concurrent limit check to pass with 1 active task")
	}

	// Case 3: Two active tasks (at limit of 2)
	db.Create(&TaskStateMock{ID: "task2", OrgID: orgID, Status: "PAUSED", CreatedAt: time.Now()})
	ok, err = limits.CheckConcurrentLimit(db)
	if err != nil {
		t.Fatalf("CheckConcurrentLimit error: %v", err)
	}
	if ok {
		t.Errorf("expected concurrent limit check to fail with 2 active tasks (at limit)")
	}

	// Case 4: Complete one task (drops active count to 1)
	db.Model(&TaskStateMock{}).Where("id = ?", "task1").Update("status", "SUCCESS")
	ok, err = limits.CheckConcurrentLimit(db)
	if err != nil {
		t.Fatalf("CheckConcurrentLimit error: %v", err)
	}
	if !ok {
		t.Errorf("expected concurrent limit check to pass after completing a task")
	}
}

func TestOrgLimitsMonthlyBudgetCheck(t *testing.T) {
	db := setupLimitsTestDB(t)
	orgID := "org-test"

	limits := &OrgLimits{
		OrgID:             orgID,
		MaxBudgetPerMonth: 10.00,
	}
	db.Create(limits)

	// Case 1: No costs yet
	ok, err := limits.CheckMonthlyBudget(db)
	if err != nil {
		t.Fatalf("CheckMonthlyBudget error: %v", err)
	}
	if !ok {
		t.Errorf("expected monthly budget check to pass with no tasks")
	}

	// Case 2: Under budget limit
	db.Create(&TaskStateMock{ID: "task1", OrgID: orgID, Status: "SUCCESS", Cost: 4.50, CreatedAt: time.Now()})
	ok, err = limits.CheckMonthlyBudget(db)
	if err != nil {
		t.Fatalf("CheckMonthlyBudget error: %v", err)
	}
	if !ok {
		t.Errorf("expected monthly budget check to pass with total cost $4.50 (limit $10.00)")
	}

	// Case 3: Exceed budget limit
	db.Create(&TaskStateMock{ID: "task2", OrgID: orgID, Status: "FAILED", Cost: 6.00, CreatedAt: time.Now()})
	ok, err = limits.CheckMonthlyBudget(db)
	if err != nil {
		t.Fatalf("CheckMonthlyBudget error: %v", err)
	}
	if ok {
		t.Errorf("expected monthly budget check to fail with total cost $10.50 (limit $10.00)")
	}
}
