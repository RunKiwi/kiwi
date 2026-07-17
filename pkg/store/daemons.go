package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"gorm.io/gorm"
)

// Errors returned by the daemon registration path. Callers map these to HTTP
// status codes; the handler must not leak which of them occurred to an
// unauthenticated caller beyond a generic rejection.
var (
	ErrJoinTokenInvalid = errors.New("join token invalid")
	ErrJoinTokenExpired = errors.New("join token expired")
	ErrJoinTokenUsed    = errors.New("join token already used")
	ErrDaemonNotFound   = errors.New("daemon not registered")
)

// hashJoinToken returns hex(sha256(token)). Join tokens are stored hashed so a
// database leak does not yield usable registration credentials.
func hashJoinToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// CreateDaemonJoinToken mints a short-lived, single-use token authorizing one
// daemon registration into orgID. The plaintext token is returned exactly once
// — only its hash is persisted — so the caller must deliver it to the operator
// (Terraform output for BYOC, internal provisioning for managed) immediately.
func (s *PostgresStore) CreateDaemonJoinToken(ctx context.Context, orgID string, ttl time.Duration) (string, error) {
	if orgID == "" {
		return "", errors.New("org id is required")
	}
	if ttl <= 0 {
		ttl = time.Hour
	}

	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	token := hex.EncodeToString(raw)

	row := &DaemonJoinToken{
		TokenHash: hashJoinToken(token),
		OrgID:     orgID,
		ExpiresAt: time.Now().Add(ttl),
		CreatedAt: time.Now(),
	}
	if err := s.db.WithContext(ctx).Create(row).Error; err != nil {
		return "", err
	}
	return token, nil
}

// RegisterDaemon redeems a join token and binds the daemon's public keys to the
// token's org.
//
// The whole redemption happens in one transaction and the token is claimed with
// a conditional UPDATE (used_at IS NULL), so two daemons racing on the same
// token cannot both register — exactly one wins.
//
// Re-registration by an already-known daemon (same Ed25519 identity) is allowed
// and refreshes its X25519 seal target, which is how key rotation works. It
// still costs a fresh join token, so a leaked identity cannot silently re-point
// credential delivery at an attacker's key.
func (s *PostgresStore) RegisterDaemon(ctx context.Context, joinToken, signPubKey, encPubKey string) (*Daemon, error) {
	if joinToken == "" || signPubKey == "" || encPubKey == "" {
		return nil, ErrJoinTokenInvalid
	}
	hash := hashJoinToken(joinToken)

	var out *Daemon
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var tok DaemonJoinToken
		if err := tx.Where("token_hash = ?", hash).First(&tok).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrJoinTokenInvalid
			}
			return err
		}
		if tok.UsedAt != nil {
			return ErrJoinTokenUsed
		}
		if time.Now().After(tok.ExpiresAt) {
			return ErrJoinTokenExpired
		}

		// Claim the token. The used_at IS NULL predicate makes this the
		// serialization point: a concurrent redemption updates 0 rows and loses.
		now := time.Now()
		res := tx.Model(&DaemonJoinToken{}).
			Where("token_hash = ? AND used_at IS NULL", hash).
			Update("used_at", now)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return ErrJoinTokenUsed
		}

		// Bind the identity to the token's org. Never trust an org from the
		// request body — it comes only from the token.
		var existing Daemon
		err := tx.Where("sign_pub_key = ?", signPubKey).First(&existing).Error
		switch {
		case err == nil:
			// Known identity: refresh the seal target and re-bind to this org.
			if err := tx.Model(&Daemon{}).Where("id = ?", existing.ID).Updates(map[string]interface{}{
				"enc_pub_key": encPubKey,
				"org_id":      tok.OrgID,
			}).Error; err != nil {
				return err
			}
			existing.EncPubKey = encPubKey
			existing.OrgID = tok.OrgID
			out = &existing
			return nil
		case errors.Is(err, gorm.ErrRecordNotFound):
			idBytes := make([]byte, 8)
			if _, err := rand.Read(idBytes); err != nil {
				return err
			}
			d := &Daemon{
				ID:         "dmn_" + hex.EncodeToString(idBytes),
				OrgID:      tok.OrgID,
				SignPubKey: signPubKey,
				EncPubKey:  encPubKey,
				CreatedAt:  now,
			}
			if err := tx.Create(d).Error; err != nil {
				return fmt.Errorf("create daemon: %w", err)
			}
			out = d
			return nil
		default:
			return err
		}
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// GetDaemonBySignPubKey resolves a daemon by its Ed25519 identity. This is the
// lookup that turns an authenticated heartbeat into an org_id.
func (s *PostgresStore) GetDaemonBySignPubKey(ctx context.Context, signPubKey string) (*Daemon, error) {
	if signPubKey == "" {
		return nil, ErrDaemonNotFound
	}
	var d Daemon
	if err := s.db.WithContext(ctx).Where("sign_pub_key = ?", signPubKey).First(&d).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrDaemonNotFound
		}
		return nil, err
	}
	return &d, nil
}

// TouchDaemon records liveness for a daemon that just authenticated.
func (s *PostgresStore) TouchDaemon(ctx context.Context, id string) error {
	now := time.Now()
	return s.db.WithContext(ctx).Model(&Daemon{}).
		Where("id = ?", id).
		Update("last_seen_at", now).Error
}
