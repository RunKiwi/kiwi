package store

import "time"

// PostMergeTelemetryPoll is one recurring (monitor, metric) telemetry poll.
// CurrentStart/CurrentEnd are advanced by the orchestrator on each report
// (Task 11); BaselineStart/BaselineEnd are fixed at creation (the pre-merge
// range) and never change. WindowEndsAt is this poll's own 4h telemetry
// deadline — independent of the parent PostMergeMonitor's 24h GitHub-native
// WindowEndsAt (Phase 1a), which is unaffected by telemetry finishing early
// or going quiet.
type PostMergeTelemetryPoll struct {
	ID            string     `gorm:"primaryKey" json:"id"`
	OrgID         string     `gorm:"index:idx_pg_telemetry_polls_due,priority:1;not null" json:"org_id"`
	MonitorID     string     `gorm:"index;not null" json:"monitor_id"`
	Provider      string     `gorm:"not null" json:"provider"`
	Query         string     `gorm:"not null" json:"query"`
	BaselineStart time.Time  `gorm:"not null" json:"baseline_start"`
	BaselineEnd   time.Time  `gorm:"not null" json:"baseline_end"`
	CurrentStart  time.Time  `gorm:"not null" json:"current_start"`
	CurrentEnd    time.Time  `gorm:"not null" json:"current_end"`
	NextPollAt    time.Time  `gorm:"index:idx_pg_telemetry_polls_due,priority:2;not null" json:"next_poll_at"`
	ClaimedAt     *time.Time `json:"claimed_at"`
	WindowEndsAt  time.Time  `gorm:"not null" json:"window_ends_at"`
	LastResult    string     `gorm:"not null;default:''" json:"last_result"`
	CreatedAt     time.Time  `gorm:"not null;default:current_timestamp" json:"created_at"`
	UpdatedAt     time.Time  `gorm:"not null;default:current_timestamp" json:"updated_at"`
}

func (PostMergeTelemetryPoll) TableName() string { return "postmerge_telemetry_polls" }
