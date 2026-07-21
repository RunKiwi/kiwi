package auth

import (
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// SharedFreeFleet is the well-known fleet id every free-tier daemon joins. Free
// work is routed to it; see the Free Tier RFC.
const SharedFreeFleet = "shared-free"

// Organization represents a tenant in the multi-tenant system.
type Organization struct {
	ID              string    `json:"id" gorm:"primaryKey"`
	Name            string    `json:"name" gorm:"uniqueIndex;not null"`
	Type            string    `json:"type" gorm:"not null;default:personal"`
	PrimaryDomain   string    `json:"primary_domain" gorm:"not null;default:''"`
	DomainJoin      bool      `json:"domain_join" gorm:"not null;default:false"`
	Plan            string    `json:"plan" gorm:"not null;default:free"`
	ActivationState string    `json:"activation_state" gorm:"not null;default:inactive"`
	CreatedAt       time.Time `json:"created_at"`
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
	OAuthProvider *string   `json:"oauth_provider,omitempty" gorm:"column:oauth_provider;uniqueIndex:idx_users_oauth,priority:1"`
	OAuthSubject  *string   `json:"oauth_subject,omitempty" gorm:"column:oauth_subject;uniqueIndex:idx_users_oauth,priority:2"`
	CreatedAt     time.Time `json:"created_at"`
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
	ID        string    `json:"id" gorm:"primaryKey"`
	OrgID     string    `json:"org_id" gorm:"index;not null"`
	Type      string    `json:"type" gorm:"not null"`                   // "provision" or "reclaim"
	Status    string    `json:"status" gorm:"not null;default:pending"` // pending, in_progress, completed, failed
	CreatedAt time.Time `json:"created_at"`
	// UpdatedAt is GORM-managed (bumped on every Update), so it marks when a row
	// was last claimed/settled. The stale-in_progress sweep uses it to find rows
	// a crashed provisioner left claimed.
	UpdatedAt time.Time `json:"updated_at"`
}

// InitAuthDB initializes the auth database tables within an existing GORM DB.
func InitAuthDB(db *gorm.DB) error {
	return db.AutoMigrate(&Organization{}, &User{}, &APIKey{}, &OrgLimits{}, &OrgProviderConfig{}, &OrgJoinRequest{}, &ProvisioningRequest{})
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
	if err := db.AutoMigrate(&Organization{}, &User{}, &APIKey{}, &OrgLimits{}, &OrgProviderConfig{}, &OrgJoinRequest{}, &ProvisioningRequest{}); err != nil {
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
