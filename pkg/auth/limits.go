package auth

import (
	"fmt"
	"time"

	"gorm.io/gorm"
)

// OrgLimits defines the resource constraints and configuration for an organization.
type OrgLimits struct {
	OrgID              string  `json:"org_id" gorm:"primaryKey;index;not null"`
	MaxConcurrentTasks int     `json:"max_concurrent_tasks"`
	MaxBudgetPerTask   float64 `json:"max_budget_per_task"`
	MaxBudgetPerMonth  float64 `json:"max_budget_per_month"`
	MaxSandboxDiskMB   int     `json:"max_sandbox_disk_mb"`
	MaxSandboxMemoryMB int     `json:"max_sandbox_memory_mb"`
	MaxSandboxCPU      float64 `json:"max_sandbox_cpu"`
	DockerImage        string  `json:"docker_image"`
	TaskTimeoutMinutes int     `json:"task_timeout_minutes"`
}

// TableName overrides the default GORM table name.
func (OrgLimits) TableName() string { return "org_limits" }

// DefaultLimits returns the fallback resource limits for any organization.
func DefaultLimits(orgID string) *OrgLimits {
	return &OrgLimits{
		OrgID:              orgID,
		MaxConcurrentTasks: 5,
		MaxBudgetPerTask:   1.00,
		MaxBudgetPerMonth:  50.00,
		MaxSandboxDiskMB:   200,
		MaxSandboxMemoryMB: 512,
		MaxSandboxCPU:      1.0,
		DockerImage:        "golang:1.21-alpine",
		TaskTimeoutMinutes: 10,
	}
}

// GetOrgLimits retrieves limits for an org from the DB, falling back to defaults.
func GetOrgLimits(db *gorm.DB, orgID string) (*OrgLimits, error) {
	var limits OrgLimits
	if err := db.Where("org_id = ?", orgID).First(&limits).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return DefaultLimits(orgID), nil
		}
		return nil, fmt.Errorf("failed to fetch org limits: %w", err)
	}

	// Apply individual defaults if specific fields are zero/empty
	if limits.MaxConcurrentTasks <= 0 {
		limits.MaxConcurrentTasks = 5
	}
	if limits.MaxBudgetPerTask <= 0 {
		limits.MaxBudgetPerTask = 1.00
	}
	if limits.MaxBudgetPerMonth <= 0 {
		limits.MaxBudgetPerMonth = 50.00
	}
	if limits.MaxSandboxDiskMB <= 0 {
		limits.MaxSandboxDiskMB = 200
	}
	if limits.MaxSandboxMemoryMB <= 0 {
		limits.MaxSandboxMemoryMB = 512
	}
	if limits.MaxSandboxCPU <= 0 {
		limits.MaxSandboxCPU = 1.0
	}
	if limits.DockerImage == "" {
		limits.DockerImage = "golang:1.21-alpine"
	}
	if limits.TaskTimeoutMinutes <= 0 {
		limits.TaskTimeoutMinutes = 10
	}

	return &limits, nil
}

// CheckConcurrentLimit checks if an organization is within its concurrent task limit.
func (l *OrgLimits) CheckConcurrentLimit(db *gorm.DB) (bool, error) {
	var activeCount int64
	// TaskState statuses that consume concurrency
	err := db.Table("task_states").
		Where("org_id = ? AND status IN ?", l.OrgID, []string{"RUNNING", "PAUSED"}).
		Count(&activeCount).Error

	if err != nil {
		return false, fmt.Errorf("failed to count active tasks: %w", err)
	}

	return int(activeCount) < l.MaxConcurrentTasks, nil
}

// CheckMonthlyBudget checks if the organization has remaining monthly budget.
// It aggregates costs of all tasks completed in the current billing month.
func (l *OrgLimits) CheckMonthlyBudget(db *gorm.DB) (bool, error) {
	now := time.Now()
	startOfMonth := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())

	var totalCost float64
	err := db.Table("task_states").
		Where("org_id = ? AND created_at >= ?", l.OrgID, startOfMonth).
		Select("COALESCE(SUM(cost), 0)").
		Row().
		Scan(&totalCost)

	if err != nil {
		return false, fmt.Errorf("failed to aggregate monthly cost: %w", err)
	}

	return totalCost < l.MaxBudgetPerMonth, nil
}
