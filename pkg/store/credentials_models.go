package store

import "time"

// Credential kinds.
const (
	CredentialLLM = "llm"
	CredentialGit = "git"
)

// Credential is an org-scoped secret (an LLM API key, a Git token, etc.) stored
// by the SaaS Control Plane. The value is encrypted at rest with AES-256-GCM;
// the plaintext is never persisted and never serialized to JSON. For delivery
// to a customer daemon it is re-sealed to that daemon's X25519 public key (see
// SealCredentialsForDaemon), so the SaaS transport never carries plaintext.
type Credential struct {
	ID    string `gorm:"primaryKey" json:"id"`
	OrgID string `gorm:"index;not null;uniqueIndex:idx_org_cred_name,priority:1" json:"org_id"`
	// Name is the env-var-style key, e.g. "ANTHROPIC_API_KEY" or "GITHUB_TOKEN".
	Name string `gorm:"not null;uniqueIndex:idx_org_cred_name,priority:2" json:"name"`
	// Kind ∈ llm|git.
	Kind string `gorm:"not null" json:"kind"`
	// EncryptedValue is AES-256-GCM ciphertext (hex). Never exposed in JSON.
	EncryptedValue string    `gorm:"not null" json:"-"`
	CreatedAt      time.Time `gorm:"not null;default:current_timestamp" json:"created_at"`
	UpdatedAt      time.Time `gorm:"not null;default:current_timestamp" json:"updated_at"`
}

func (Credential) TableName() string { return "credentials" }
