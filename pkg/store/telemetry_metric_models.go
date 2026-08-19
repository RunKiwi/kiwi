package store

import "time"

const (
	ComparisonHigherIsBetter = "higher_is_better"
	ComparisonLowerIsBetter  = "lower_is_better"
)

// TelemetryMetric is an org-level, operator-configured named metric a
// PostMergeMonitor's telemetry poll can be pointed at. Kiwi does not
// discover metrics from a telemetry backend's schema (Phase 1a's design
// doc calls that "a spike, not a plannable task") — an operator names one
// via the dashboard, and a monitor's originating task Intent picks among
// an org's configured metrics for the same repo (see Task 11).
type TelemetryMetric struct {
	ID                  string    `gorm:"primaryKey" json:"id"`
	OrgID               string    `gorm:"uniqueIndex:idx_telemetry_metrics_org_repo_name,priority:1;not null" json:"org_id"`
	Repo                string    `gorm:"uniqueIndex:idx_telemetry_metrics_org_repo_name,priority:2;not null" json:"repo"`
	Name                string    `gorm:"uniqueIndex:idx_telemetry_metrics_org_repo_name,priority:3;not null" json:"name"`
	Provider            string    `gorm:"not null" json:"provider"`
	Query               string    `gorm:"not null" json:"query"`
	ComparisonDirection string    `gorm:"not null" json:"comparison_direction"`
	CreatedAt           time.Time `gorm:"not null;default:current_timestamp" json:"created_at"`
	UpdatedAt           time.Time `gorm:"not null;default:current_timestamp" json:"updated_at"`
}

func (TelemetryMetric) TableName() string { return "telemetry_metrics" }
