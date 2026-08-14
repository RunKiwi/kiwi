// SPDX-License-Identifier: LicenseRef-Kiwi-BSL-1.1
// Copyright (c) 2026 RunKiwi. Licensed under the Business Source License 1.1.
// See ee/LICENSE. This is Control Plane code and is NOT Apache-2.0.

package auth

import (
	"time"

	"github.com/glebarez/sqlite"
	"github.com/ibreakthecloud/kiwi/pkg/store"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// SharedFreeFleet is the well-known fleet id every free-tier daemon joins. Free
// work is routed to it. Defined in pkg/store so the fleet-type check and this
// constant can never disagree.
const SharedFreeFleet = store.SharedFreeFleet

// Organization represents a tenant in the multi-tenant system.
type Organization struct {
	ID              string `json:"id" gorm:"primaryKey"`
	Name            string `json:"name" gorm:"uniqueIndex;not null"`
	Type            string `json:"type" gorm:"not null;default:personal"`
	PrimaryDomain   string `json:"primary_domain" gorm:"not null;default:''"`
	DomainJoin      bool   `json:"domain_join" gorm:"not null;default:false"`
	Plan            string `json:"plan" gorm:"not null;default:free"`
	ActivationState string `json:"activation_state" gorm:"not null;default:active"`
	// PRCommentMode selects what a review comment on a Kiwi pull request does:
	// off | mention | any. See pkg/store/pr_comment_mode.go; the default is
	// mention, so Kiwi acts only when it is spoken to.
	PRCommentMode string    `gorm:"not null;default:mention" json:"pr_comment_mode"`
	CreatedAt     time.Time `json:"created_at"`
	// AbuseStrikes counts recent abuse signals within AbuseStrikeWindow; the org
	// is auto-suspended once it reaches the threshold. It decays (resets) when a
	// strike arrives after the window, and is cleared on suspend. See
	// RecordAbuseStrike — a single strike is a weak signal, so we require repeats.
	AbuseStrikes int        `json:"abuse_strikes" gorm:"not null;default:0"`
	LastAbuseAt  *time.Time `json:"last_abuse_at"`
}

// CanRun returns true if the organization is active and allowed to run tasks.
func (o *Organization) CanRun() bool {
	return o.ActivationState == "active"
}

// TableName overrides the default GORM table name.
func (Organization) TableName() string { return "organizations" }

// User represents an authenticated user belonging to an organization.
type User struct {
	ID    string `json:"id" gorm:"primaryKey"`
	Email string `json:"email" gorm:"uniqueIndex;not null"`
	Name  string `json:"name"`
	OrgID string `json:"org_id" gorm:"index;not null"`
	Role  string `json:"role" gorm:"not null;default:member"` // "admin" or "member"
	// Explicit column names: without them GORM maps OAuthProvider ->
	// "o_auth_provider", but migration 0008 (and the raw WHERE in oauth.go)
	// use "oauth_provider". Pin the names so struct ops and SQL agree.
	OAuthProvider *string `json:"oauth_provider,omitempty" gorm:"column:oauth_provider;uniqueIndex:idx_users_oauth,priority:1"`
	OAuthSubject  *string `json:"oauth_subject,omitempty" gorm:"column:oauth_subject;uniqueIndex:idx_users_oauth,priority:2"`
	// GitHubLogin is the user's GitHub username, captured at sign-in.
	//
	// Nullable rather than empty-string: a Google user has no GitHub account,
	// and so does every user who signed up before this column existed — the
	// login was fetched and discarded, so there is nothing to backfill from.
	// Recording "" would make "no account" and "we never captured it" the same
	// value.
	//
	// Deliberately NOT unique and NOT an identity key. GitHub allows an account
	// to be renamed and the freed name to be re-registered by someone else, so a
	// login is a label that can move between people. OAuthSubject (the numeric
	// id) remains the thing that identifies the account.
	GitHubLogin *string   `json:"github_login,omitempty" gorm:"column:github_login;index"`
	CreatedAt   time.Time `json:"created_at"`
	// SignInCount and LastSignInAt are bumped once per completed OAuth login
	// (ee/auth/oauth.go handleOAuthCallback) — a real, infrequent event, not
	// per-request activity.
	SignInCount  int        `json:"sign_in_count" gorm:"not null;default:0"`
	LastSignInAt *time.Time `json:"last_sign_in_at"`
	// LastSeenAt is set by recordDashboardActivity (dashboard_session.go)
	// from cookie-authenticated requests only — API keys (CLI/SDK/daemon
	// traffic) never touch it, so it reflects browser dashboard use, and is
	// more current than LastSignInAt since one login's cookie can span days.
	LastSeenAt *time.Time `json:"last_seen_at"`
}

// TableName overrides the default GORM table name.
func (User) TableName() string { return "users" }

// APIKey represents a hashed API key associated with a user.
type APIKey struct {
	ID        string     `json:"id" gorm:"primaryKey"`
	KeyHash   string     `json:"-" gorm:"uniqueIndex;not null"` // SHA-256 hash of the plaintext key
	UserID    string     `json:"user_id" gorm:"index;not null"`
	Label     string     `json:"label"`
	CreatedAt time.Time  `json:"created_at"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
	RevokedAt *time.Time `json:"revoked_at,omitempty"`
}

// TableName overrides the default GORM table name.
func (APIKey) TableName() string { return "api_keys" }

// IsExpired returns true if the key has passed its expiration date.
func (k *APIKey) IsExpired() bool {
	if k.ExpiresAt == nil {
		return false
	}
	return time.Now().After(*k.ExpiresAt)
}

// IsRevoked returns true if the key has been revoked.
func (k *APIKey) IsRevoked() bool {
	return k.RevokedAt != nil
}

// OrgJoinRequest represents a request by a user to join an organization.
type OrgJoinRequest struct {
	ID        string    `json:"id" gorm:"primaryKey"`
	OrgID     string    `json:"org_id" gorm:"index;not null"`
	UserEmail string    `json:"user_email" gorm:"not null"`
	Status    string    `json:"status" gorm:"not null;default:pending"` // "pending", "approved", "denied"
	CreatedAt time.Time `json:"created_at"`
}

// TableName overrides the default GORM table name.
func (OrgJoinRequest) TableName() string { return "org_join_requests" }

// ProvisioningRequest represents a request to provision or reclaim a per-org daemon VM.
type ProvisioningRequest struct {
	ID     string `json:"id" gorm:"primaryKey"`
	OrgID  string `json:"org_id" gorm:"index;not null"`
	Type   string `json:"type" gorm:"not null"`                   // "provision" or "reclaim"
	Status string `json:"status" gorm:"not null;default:pending"` // pending, in_progress, completed, failed
	// Error records why a failed request failed. A failed request is terminal and
	// is never retried, so without this the reason exists only in a log line on
	// the provisioning host — and the org whose runner never started has no way to
	// see it. It is surfaced as the blocked reason on the tasks left stranded.
	Error     string    `json:"error,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	// UpdatedAt is GORM-managed (bumped on every Update), so it marks when a row
	// was last claimed/settled. The stale-in_progress sweep uses it to find rows
	// a crashed provisioner left claimed.
	UpdatedAt time.Time `json:"updated_at"`
}

// InitAuthDB initializes the auth database tables within an existing GORM DB.
func InitAuthDB(db *gorm.DB) error {
	return db.AutoMigrate(&Organization{}, &User{}, &APIKey{}, &OrgLimits{}, &OrgProviderConfig{}, &OrgJoinRequest{}, &ProvisioningRequest{}, &store.Fleet{})
}

// OpenDB initializes GORM with pure-Go SQLite and runs all migrations
// (auth tables + any additional models passed in).
func OpenDB(dbPath string, additionalModels ...interface{}) (*gorm.DB, error) {
	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Info),
	})
	if err != nil {
		return nil, err
	}

	// Migrate auth models
	if err := db.AutoMigrate(&Organization{}, &User{}, &APIKey{}, &OrgLimits{}, &OrgProviderConfig{}, &OrgJoinRequest{}, &ProvisioningRequest{}, &store.Fleet{}); err != nil {
		return nil, err
	}

	// Migrate any additional models passed by the caller
	if len(additionalModels) > 0 {
		if err := db.AutoMigrate(additionalModels...); err != nil {
			return nil, err
		}
	}

	return db, nil
}
