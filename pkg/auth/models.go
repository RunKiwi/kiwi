package auth

import (
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Organization represents a tenant in the multi-tenant system.
type Organization struct {
	ID        string    `json:"id" gorm:"primaryKey"`
	Name      string    `json:"name" gorm:"uniqueIndex;not null"`
	CreatedAt time.Time `json:"created_at"`
}

// TableName overrides the default GORM table name.
func (Organization) TableName() string { return "organizations" }

// User represents an authenticated user belonging to an organization.
type User struct {
	ID        string    `json:"id" gorm:"primaryKey"`
	Email     string    `json:"email" gorm:"uniqueIndex;not null"`
	Name      string    `json:"name"`
	OrgID     string    `json:"org_id" gorm:"index;not null"`
	Role      string    `json:"role" gorm:"not null;default:member"` // "admin" or "member"
	CreatedAt time.Time `json:"created_at"`
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

// InitAuthDB initializes the auth database tables within an existing GORM DB.
func InitAuthDB(db *gorm.DB) error {
	return db.AutoMigrate(&Organization{}, &User{}, &APIKey{}, &OrgLimits{}, &OrgProviderConfig{})
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
	if err := db.AutoMigrate(&Organization{}, &User{}, &APIKey{}, &OrgLimits{}, &OrgProviderConfig{}); err != nil {
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
