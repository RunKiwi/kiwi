package auth

import (
	"fmt"
	"time"

	"gorm.io/gorm"
)

// OrgLimits defines the resource constraints and configuration for an organization.
// This is a read-through struct. The canonical schema is store.OrgLimits.
type OrgLimits struct {
	OrgID                   string  `json:"org_id" gorm:"primaryKey;index;not null"`
	MaxConcurrentJobs       int     `json:"max_concurrent_jobs"`
	MaxBudgetPerJob         float64 `json:"max_budget_per_job"`
	MaxBudgetPerMonth       float64 `json:"max_budget_per_month"`
	MaxAgentMinutesPerMonth float64 `json:"max_agent_minutes_per_month"`
	MaxWorkersPerJob        int     `json:"max_workers_per_job"`
	TaskTimeoutSeconds      int     `json:"task_timeout_seconds"`
	MaxSandboxDiskMB        int     `json:"max_sandbox_disk_mb"`
}

// TableName overrides the default GORM table name.
func (OrgLimits) TableName() string { return "org_limits" }

// DefaultLimits returns the fallback resource limits for any organization.
func DefaultLimits(orgID string) *OrgLimits {
	return &OrgLimits{
		OrgID:                   orgID,
		MaxConcurrentJobs:       10,
		MaxBudgetPerJob:         5.00,
		MaxBudgetPerMonth:       500.00,
		MaxAgentMinutesPerMonth: 0,
		MaxWorkersPerJob:        8,
		TaskTimeoutSeconds:      1800,
		MaxSandboxDiskMB:        2048,
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
	if limits.MaxConcurrentJobs <= 0 {
		limits.MaxConcurrentJobs = 10
	}
	if limits.MaxBudgetPerJob <= 0 {
		limits.MaxBudgetPerJob = 5.00
	}
	if limits.MaxBudgetPerMonth <= 0 {
		limits.MaxBudgetPerMonth = 500.00
	}
	if limits.MaxWorkersPerJob <= 0 {
		limits.MaxWorkersPerJob = 8
	}
	if limits.TaskTimeoutSeconds <= 0 {
		limits.TaskTimeoutSeconds = 1800
	}
	if limits.MaxSandboxDiskMB <= 0 {
		limits.MaxSandboxDiskMB = 2048
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

	return int(activeCount) < l.MaxConcurrentJobs, nil
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
