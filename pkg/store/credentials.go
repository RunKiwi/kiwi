package store

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/ibreakthecloud/kiwi/pkg/crypto"
	"gorm.io/gorm/clause"
)

// SaveCredential upserts an org-scoped secret, encrypting the plaintext value at
// rest (AES-256-GCM). The plaintext is never persisted. The write is an atomic
// upsert (ON CONFLICT over the unique (org_id, name) index) so concurrent saves
// of the same credential cannot race into a unique-constraint violation.
func (s *PostgresStore) SaveCredential(ctx context.Context, orgID, name, kind, plaintext string) error {
	enc, err := crypto.EncryptAtRest(plaintext)
	if err != nil {
		return err
	}
	now := time.Now()

	idBytes := make([]byte, 8)
	if _, err := rand.Read(idBytes); err != nil {
		return err
	}

	cred := &Credential{
		ID:             "cred_" + hex.EncodeToString(idBytes),
		OrgID:          orgID,
		Name:           name,
		Kind:           kind,
		EncryptedValue: enc,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	return s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "org_id"}, {Name: "name"}},
		DoUpdates: clause.Assignments(map[string]interface{}{
			"kind":            kind,
			"encrypted_value": enc,
			"updated_at":      now,
		}),
	}).Create(cred).Error
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

// SealCredentialsForDaemon gathers an org's credentials, decrypts them, and
// seals the bundle to the daemon's X25519 public key.
//
// extra carries credentials that are not the org's own — today, Kiwi-owned
// platform keys. It is passed in rather than resolved here on purpose: deciding
// whether a daemon may receive a Kiwi key needs the fleet type, the leased
// model, and the org's remaining allowance, none of which belong in the store's
// credential layer. The caller decides; this function only seals.
//
// extra entries win over org entries of the same name, so a task routed to a
// Kiwi-funded model uses Kiwi's key even if the org also has one connected.
func (s *PostgresStore) SealCredentialsForDaemon(ctx context.Context, orgID string, daemonPubKey *ecdh.PublicKey, extra map[string]string) (string, error) {
	creds, err := s.ListCredentials(ctx, orgID)
	if err != nil {
		return "", err
	}
	if len(creds) == 0 && len(extra) == 0 {
		return "", nil
	}

	bundle := make(map[string]string, len(creds)+len(extra))
	for _, c := range creds {
		pt, err := crypto.DecryptAtRest(c.EncryptedValue)
		if err != nil {
			return "", err
		}
		bundle[c.Name] = pt
	}
	for name, value := range extra {
		bundle[name] = value
	}

	payload, err := json.Marshal(bundle)
	if err != nil {
		return "", err
	}
	return crypto.SealToPublicKey(daemonPubKey, payload)
}
