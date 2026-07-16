package store

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"time"

	"github.com/ibreakthecloud/kiwi/pkg/crypto"
	"gorm.io/gorm"
)

// SaveCredential upserts an org-scoped secret, encrypting the plaintext value at
// rest (AES-256-GCM). The plaintext is never persisted.
func (s *PostgresStore) SaveCredential(ctx context.Context, orgID, name, kind, plaintext string) error {
	enc, err := crypto.EncryptAtRest(plaintext)
	if err != nil {
		return err
	}
	now := time.Now()

	var existing Credential
	err = s.db.WithContext(ctx).Where("org_id = ? AND name = ?", orgID, name).First(&existing).Error
	if err == nil {
		return s.db.WithContext(ctx).Model(&Credential{}).
			Where("id = ?", existing.ID).
			Updates(map[string]interface{}{
				"kind":            kind,
				"encrypted_value": enc,
				"updated_at":      now,
			}).Error
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}

	idBytes := make([]byte, 8)
	if _, err := rand.Read(idBytes); err != nil {
		return err
	}
	return s.db.WithContext(ctx).Create(&Credential{
		ID:             "cred_" + hex.EncodeToString(idBytes),
		OrgID:          orgID,
		Name:           name,
		Kind:           kind,
		EncryptedValue: enc,
		CreatedAt:      now,
		UpdatedAt:      now,
	}).Error
}

// ListCredentials returns an org's credentials (metadata only; values stay
// encrypted in EncryptedValue and are never returned in plaintext here).
func (s *PostgresStore) ListCredentials(ctx context.Context, orgID string) ([]Credential, error) {
	var creds []Credential
	if err := s.db.WithContext(ctx).Where("org_id = ?", orgID).Order("name ASC").Find(&creds).Error; err != nil {
		return nil, err
	}
	return creds, nil
}

// GetCredentialPlaintext decrypts and returns a single credential's value.
func (s *PostgresStore) GetCredentialPlaintext(ctx context.Context, orgID, name string) (string, error) {
	var cred Credential
	if err := s.db.WithContext(ctx).Where("org_id = ? AND name = ?", orgID, name).First(&cred).Error; err != nil {
		return "", err
	}
	return crypto.DecryptAtRest(cred.EncryptedValue)
}

// SealCredentialsForDaemon gathers all of an org's credentials, decrypts them
// from at-rest storage, and re-seals them as a single JSON map to the daemon's
// X25519 public key. The returned base64 blob is safe to carry over the SaaS
// transport (e.g. in HeartbeatRes.EncryptedCreds): only the daemon holding the
// matching private key can open it. Returns "" when the org has no credentials.
func (s *PostgresStore) SealCredentialsForDaemon(ctx context.Context, orgID string, daemonPubKey *ecdh.PublicKey) (string, error) {
	creds, err := s.ListCredentials(ctx, orgID)
	if err != nil {
		return "", err
	}
	if len(creds) == 0 {
		return "", nil
	}

	bundle := make(map[string]string, len(creds))
	for _, c := range creds {
		pt, err := crypto.DecryptAtRest(c.EncryptedValue)
		if err != nil {
			return "", err
		}
		bundle[c.Name] = pt
	}

	payload, err := json.Marshal(bundle)
	if err != nil {
		return "", err
	}
	return crypto.SealToPublicKey(daemonPubKey, payload)
}
