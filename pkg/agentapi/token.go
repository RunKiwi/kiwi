// Package agentapi is the control-plane boundary a sandbox agent uses to reach
// the control plane. It exposes a small HTTP/JSON API (AppendEvent, Checkpoint,
// FetchSecret, ReportResult) authorized by a short-lived, per-job scoped token —
// a token can only act on its own job_id. (Issue #34. A gRPC transport can be
// swapped behind the same client/server split later.)
package agentapi

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"time"

	"gorm.io/gorm"
)

// JobToken is a hashed, per-job, expiring credential minted when a job is
// provisioned. Only the SHA-256 hash is stored, never the plaintext.
type JobToken struct {
	ID        string    `gorm:"primaryKey" json:"id"`
	TokenHash string    `gorm:"uniqueIndex;not null" json:"-"`
	JobID     string    `gorm:"index;not null" json:"job_id"`
	OrgID     string    `gorm:"not null" json:"org_id"`
	ExpiresAt time.Time `gorm:"not null" json:"expires_at"`
	CreatedAt time.Time `json:"created_at"`
}

// JobClaims is the identity a validated job token carries.
type JobClaims struct {
	JobID string
	OrgID string
}

var (
	ErrInvalidToken = errors.New("invalid job token")
	ErrExpiredToken = errors.New("job token expired")
)

func hashToken(plaintext string) string {
	sum := sha256.Sum256([]byte(plaintext))
	return hex.EncodeToString(sum[:])
}

// MintJobToken creates a scoped token for jobID valid for ttl and returns the
// plaintext (shown once; only its hash is persisted).
func MintJobToken(db *gorm.DB, jobID, orgID string, ttl time.Duration) (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	plaintext := "kiwijob_" + hex.EncodeToString(raw)
	jt := &JobToken{
		ID:        hex.EncodeToString(raw[:8]),
		TokenHash: hashToken(plaintext),
		JobID:     jobID,
		OrgID:     orgID,
		ExpiresAt: time.Now().Add(ttl),
		CreatedAt: time.Now(),
	}
	if err := db.Create(jt).Error; err != nil {
		return "", err
	}
	return plaintext, nil
}

// ValidateJobToken resolves a plaintext token to its claims, rejecting unknown
// or expired tokens. Constant-time lookup is unnecessary here because the lookup
// key is already a SHA-256 hash of the secret.
func ValidateJobToken(db *gorm.DB, plaintext string) (*JobClaims, error) {
	if plaintext == "" {
		return nil, ErrInvalidToken
	}
	var jt JobToken
	if err := db.First(&jt, "token_hash = ?", hashToken(plaintext)).Error; err != nil {
		return nil, ErrInvalidToken
	}
	if time.Now().After(jt.ExpiresAt) {
		return nil, ErrExpiredToken
	}
	return &JobClaims{JobID: jt.JobID, OrgID: jt.OrgID}, nil
}
