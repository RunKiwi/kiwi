// pkg/store/slack_models.go
package store

import "time"

// CredentialSlack marks a Slack bot token, stored the same way an LLM key or
// git token is (SaveCredential/GetCredentialPlaintext, AES-256-GCM at rest).
const CredentialSlack = "slack"

// SlackInstallation links one Kiwi org to one Slack workspace ("team"),
// mirroring GitHubInstallation's shape: the team, not the channel, is the
// unit OAuth grants against.
type SlackInstallation struct {
	TeamID            string    `gorm:"primaryKey" json:"team_id"`
	OrgID             string    `gorm:"index;not null" json:"org_id"`
	TeamName          string    `gorm:"not null;default:''" json:"team_name"`
	InstalledByUserID string    `gorm:"not null;default:''" json:"installed_by_user_id"`
	CreatedAt         time.Time `gorm:"not null;default:current_timestamp" json:"created_at"`
	UpdatedAt         time.Time `gorm:"not null;default:current_timestamp" json:"updated_at"`
}

func (SlackInstallation) TableName() string { return "slack_installations" }

// SlackChannelBinding pins one Slack channel to a repo (and optionally a
// default test command / ref), set once by an admin so a bare "@runkiwi fix
// this bug" in that channel knows what "this" refers to.
type SlackChannelBinding struct {
	ID             string    `gorm:"primaryKey" json:"id"`
	OrgID          string    `gorm:"index;not null" json:"org_id"`
	TeamID         string    `gorm:"not null;uniqueIndex:idx_scb_channel,priority:1" json:"team_id"`
	ChannelID      string    `gorm:"not null;uniqueIndex:idx_scb_channel,priority:2" json:"channel_id"`
	RepoURL        string    `gorm:"not null" json:"repo_url"`
	DefaultTestCmd string    `gorm:"not null;default:''" json:"default_test_cmd"`
	DefaultRef     string    `gorm:"not null;default:''" json:"default_ref"`
	CreatedBy      string    `gorm:"not null;default:''" json:"created_by"`
	CreatedAt      time.Time `gorm:"not null;default:current_timestamp" json:"created_at"`
}

func (SlackChannelBinding) TableName() string { return "slack_channel_bindings" }

// SlackTriggeredTask maps a Kiwi task back to the Slack thread that started
// it. Not one row per thread — a thread can accumulate several tasks over
// time (fork, new), so callers always want the LATEST row for a ThreadTS.
type SlackTriggeredTask struct {
	ID                string    `gorm:"primaryKey" json:"id"`
	OrgID             string    `gorm:"index;not null" json:"org_id"`
	TeamID            string    `gorm:"not null" json:"team_id"`
	ChannelID         string    `gorm:"not null" json:"channel_id"`
	ThreadTS          string    `gorm:"not null" json:"thread_ts"`
	ParentTaskID      *string   `json:"parent_task_id,omitempty"`
	QueuedTaskID      string    `gorm:"not null;default:''" json:"queued_task_id"`
	StatusMessageTS   string    `gorm:"not null;default:''" json:"status_message_ts"`
	LastStatus        string    `gorm:"not null;default:''" json:"last_status"`
	InvestigationOnly bool      `gorm:"not null;default:false" json:"investigation_only"`
	CreatedAt         time.Time `gorm:"not null;default:current_timestamp" json:"created_at"`
	UpdatedAt         time.Time `gorm:"not null;default:current_timestamp" json:"updated_at"`
}

func (SlackTriggeredTask) TableName() string { return "slack_triggered_tasks" }
