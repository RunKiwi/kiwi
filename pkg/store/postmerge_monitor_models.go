package store

import "time"

// Post-Merge Verification (Phase 1a) statuses. Phase 1a has no telemetry
// integration, so its signals are binary — a bad GitHub-native signal means
// REGRESSION, otherwise the window elapsing clean means VERIFIED. There is
// deliberately no INCONCLUSIVE/ERRORED here; those require a statistical
// significance judgment that only exists once telemetry polling (Phase 1b)
// is built.
const (
	MonitorStatusMonitoring = "MONITORING"
	MonitorStatusVerified   = "VERIFIED"
	MonitorStatusRegression = "REGRESSION"
)

// PostMergeMonitor tracks one merged, Kiwi-authored PR from merge through a
// fixed monitoring window. Exactly one row exists per (org_id, job_id) — a
// job merges once, so a monitor is created once, by the merge webhook.
type PostMergeMonitor struct {
	ID                string     `gorm:"primaryKey" json:"id"`
	OrgID             string     `gorm:"uniqueIndex:idx_postmerge_monitors_org_job,priority:1;not null" json:"org_id"`
	JobID             string     `gorm:"uniqueIndex:idx_postmerge_monitors_org_job,priority:2;not null" json:"job_id"`
	Repo              string     `gorm:"not null" json:"repo"`
	PRNumber          int        `gorm:"not null" json:"pr_number"`
	MergeCommitSHA    string     `gorm:"index;not null" json:"merge_commit_sha"`
	Status            string     `gorm:"index:idx_postmerge_monitors_status_window,priority:1;not null;default:MONITORING" json:"status"`
	VerdictEvidence   string     `gorm:"not null;default:''" json:"verdict_evidence"`
	RemediationTaskID *string    `json:"remediation_task_id"`
	DeployedAt        time.Time  `gorm:"not null" json:"deployed_at"`
	WindowEndsAt      time.Time  `gorm:"index:idx_postmerge_monitors_status_window,priority:2;not null" json:"window_ends_at"`
	FinalizedAt       *time.Time `json:"finalized_at"`
	CreatedAt         time.Time  `gorm:"not null;default:current_timestamp" json:"created_at"`
	UpdatedAt         time.Time  `gorm:"not null;default:current_timestamp" json:"updated_at"`
}

func (PostMergeMonitor) TableName() string { return "postmerge_monitors" }
