// pkg/store/slack_models.go
package store

import (
	"fmt"
	"time"

	"github.com/ibreakthecloud/kiwi/pkg/crypto"
)

// CredentialSlack marks a Slack bot token, stored the same way an LLM key or
// git token is (SaveCredential/GetCredentialPlaintext, AES-256-GCM at rest).
//
// Deprecated: a bot token is per-WORKSPACE, not per-org — the Credential
// table is keyed uniquely on (org_id, name), so an org that connects a
// second Slack workspace silently overwrote the first's token here. Kept
// only so a row saved before EncryptedBotToken existed keeps decrypting;
// every write path now uses SlackInstallation.EncryptedBotToken instead.
const CredentialSlack = "slack"

// SlackInstallation links one Kiwi org to one Slack workspace ("team"),
// mirroring GitHubInstallation's shape: the team, not the channel, is the
// unit OAuth grants against. An org can connect more than one workspace, so
// this is intentionally one row per team, not per org.
type SlackInstallation struct {
	TeamID            string `gorm:"primaryKey" json:"team_id"`
	OrgID             string `gorm:"index;not null" json:"org_id"`
	TeamName          string `gorm:"not null;default:''" json:"team_name"`
	InstalledByUserID string `gorm:"not null;default:''" json:"installed_by_user_id"`
	// EncryptedBotToken is this workspace's own bot token (AES-256-GCM,
	// same as Credential.EncryptedValue), stored on the team-scoped
	// installation row rather than the org-scoped Credential table — see
	// CredentialSlack's deprecation note for why. Never serialized.
	EncryptedBotToken string    `gorm:"not null;default:''" json:"-"`
	CreatedAt         time.Time `gorm:"not null;default:current_timestamp" json:"created_at"`
	UpdatedAt         time.Time `gorm:"not null;default:current_timestamp" json:"updated_at"`
}

func (SlackInstallation) TableName() string { return "slack_installations" }

// DecryptBotToken decrypts this installation's own bot token. No DB access —
// every caller reaches this having already looked the row up by team_id, so
// this replaces what used to be a second round trip through
// GetCredentialPlaintext(orgID, "SLACK_BOT_TOKEN"), which answered for the
// wrong workspace whenever an org had connected more than one.
func (i *SlackInstallation) DecryptBotToken() (string, error) {
	if i.EncryptedBotToken == "" {
		return "", fmt.Errorf("no bot token stored for team %s", i.TeamID)
	}
	return crypto.DecryptAtRest(i.EncryptedBotToken)
}

// SlackChannelBinding pins one Slack channel to a repo (and optionally a
// default test command / ref), set once by an admin so a bare "@runkiwi fix
// this bug" in that channel knows what "this" refers to.
type SlackChannelBinding struct {
	ID             string `gorm:"primaryKey" json:"id"`
	OrgID          string `gorm:"index;not null" json:"org_id"`
	TeamID         string `gorm:"not null;uniqueIndex:idx_scb_channel,priority:1" json:"team_id"`
	ChannelID      string `gorm:"not null;uniqueIndex:idx_scb_channel,priority:2" json:"channel_id"`
	RepoURL        string `gorm:"not null" json:"repo_url"`
	DefaultTestCmd string `gorm:"not null;default:''" json:"default_test_cmd"`
	DefaultRef     string `gorm:"not null;default:''" json:"default_ref"`
	// DefaultModel and DefaultArchitectModel pin the Implementer/Architect an
	// @mention in this channel runs, the same way DefaultTestCmd/DefaultRef
	// pin a repo default. Empty means "not configured" — the trigger path
	// leaves PlanRequest.Model/ArchitectModel empty in that case too, which
	// is what makes SubmitPlan's own runtime catalog auto-pick run; a
	// pre-configured value here always wins over that auto-pick, never the
	// other way round.
	DefaultModel          string    `gorm:"not null;default:''" json:"default_model"`
	DefaultArchitectModel string    `gorm:"not null;default:''" json:"default_architect_model"`
	CreatedBy             string    `gorm:"not null;default:''" json:"created_by"`
	CreatedAt             time.Time `gorm:"not null;default:current_timestamp" json:"created_at"`
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

// SlackProcessedEvent is a claim ledger for Slack Events API delivery ids —
// see RecordSlackEvent. Its only purpose is the unique index on event_id;
// nothing ever reads a row back out.
type SlackProcessedEvent struct {
	EventID   string    `gorm:"primaryKey" json:"event_id"`
	CreatedAt time.Time `gorm:"not null;default:current_timestamp" json:"created_at"`
}

func (SlackProcessedEvent) TableName() string { return "slack_processed_events" }
