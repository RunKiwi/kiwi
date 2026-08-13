// SPDX-License-Identifier: LicenseRef-Kiwi-BSL-1.1
// Copyright (c) 2026 RunKiwi. Licensed under the Business Source License 1.1.
// See ee/LICENSE. This is Control Plane code and is NOT Apache-2.0.

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

// FreeLimits returns the resource caps for a Free-tier organization. This is the
// single source of truth for the Free profile — org creation writes it, and
// GetOrgLimits falls back to it for a free-plan org that has no explicit row.
func FreeLimits(orgID string) *OrgLimits {
	return &OrgLimits{
		OrgID:             orgID,
		MaxConcurrentJobs: 1,
		MaxWorkersPerJob:  2,
		// Raised from 0.50 to 2.00, 2026-08-13. The free-fleet daemon's own
		// -session-budget flag defaulted to $5.00 and, until a bug in
		// ee/provisioner's launchArgs was fixed the same day, that $5.00 was
		// what every Free session actually ran under — never the org's real
		// 0.50 cap, which was only enforced (inconsistently — see
		// pkg/store.effectiveOrgLimits) at lease time against accumulated
		// spend. Session mode's own round/timeout math was calibrated against
		// that de facto $5 ceiling ("$5 buys three or four rounds" — see the
		// TaskTimeoutSeconds comment below), not 0.50. Once the daemon-side bug
		// was fixed, 0.50 became the real, binding cap for the first time — and
		// separately, the Architect gained read_file/grep tool calls the same
		// day, adding real exploration cost on top of an already-tight number.
		// 2.00 is a deliberate step down from the de facto $5 Free had been
		// running at, not a guess: real headroom for both, still 60% cheaper
		// than what was actually happening in production for weeks.
		MaxBudgetPerJob: 2.00,
		// A real dollar value, not the 0 sentinel that GetOrgLimits rewrites at
		// read time — this profile is also returned directly as a fallback, where
		// 0 would read as a hard $0/month cap and block every submit. The Free
		// compute lever is agent-minutes below, not the dollar budget.
		MaxBudgetPerMonth:       500.00,
		MaxAgentMinutesPerMonth: 500,
		// Twenty minutes, raised from ten.
		//
		// Ten was chosen for the single-file loop, where it is close to
		// unreachable: six Actor steps at $0.50 rarely take that long unless the
		// test suite is slow, so the step and dollar rails bind first. Session
		// mode has different economics — $5 buys three or four rounds, each with
		// an Architect plan, an agentic Implementer and a review — and there the
		// clock was binding, cutting off runs that still had budget to spend.
		// (That "$5" is not a stale reference: see the MaxBudgetPerJob comment
		// above — it is the number this was actually calibrated against.)
		//
		// Still short of the 1800 every other plan gets (DefaultLimits), because
		// wall clock is what Free meters: this is 20 of the org's 500
		// agent-minutes per month, so a month is ~25 maximum-length tasks rather
		// than ~50.
		TaskTimeoutSeconds: 1200,
		MaxSandboxDiskMB:   512,
	}
}

// GetOrgLimits retrieves limits for an org from the DB, falling back to defaults.
func GetOrgLimits(db *gorm.DB, orgID string) (*OrgLimits, error) {
	var limits OrgLimits
	if err := db.Where("org_id = ?", orgID).First(&limits).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			// No explicit limits row: a free-plan org gets the Free profile, any
			// other plan the generic defaults. This keeps the Free caps correct for
			// orgs created before the profile was written at signup — no backfill
			// needed for the fallback to be right.
			var org Organization
			if e := db.Select("plan").First(&org, "id = ?", orgID).Error; e == nil && org.Plan == "free" {
				return FreeLimits(orgID), nil
			}
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
