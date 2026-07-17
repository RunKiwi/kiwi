package store

import "time"

// Daemon is a registered Data Plane runner. It is the binding between a
// daemon's cryptographic identity and the org whose work it may lease.
//
// The daemon has two keypairs (see pkg/crypto), and this row stores the public
// half of each:
//
//   - SignPubKey (Ed25519) is the daemon's *identity*. Every heartbeat is signed
//     with the matching private key, so possession of that key is what proves
//     "I am this daemon". It is unique across the fleet and is the lookup key
//     that resolves a request to an org.
//   - EncPubKey (X25519) is the *seal target*. Credentials are sealed to it so
//     only this daemon can open them.
//
// Both are base64(raw key bytes) — the same encoding the daemon sends in its
// heartbeat body.
type Daemon struct {
	ID    string `gorm:"primaryKey" json:"id"`
	OrgID string `gorm:"index;not null" json:"org_id"`
	// SignPubKey is the Ed25519 identity. Unique: two daemons cannot share an
	// identity, and it is how a heartbeat resolves to an org.
	SignPubKey string `gorm:"uniqueIndex;not null" json:"sign_pub_key"`
	// EncPubKey is the X25519 key credentials are sealed to.
	EncPubKey string `gorm:"not null" json:"enc_pub_key"`
	// LastSeenAt is refreshed on every authenticated heartbeat.
	LastSeenAt *time.Time `json:"last_seen_at"`
	CreatedAt  time.Time  `gorm:"not null;default:current_timestamp" json:"created_at"`
}

func (Daemon) TableName() string { return "daemons" }

// DaemonJoinToken is a short-lived, org-bound, single-use credential that
// authorizes one daemon registration.
//
// It exists to close the trust-on-first-use hole: without it, "boot and
// self-register" lets anyone who discovers the registration endpoint enrol a
// rogue daemon and start receiving another org's tasks and sealed credentials.
// The token is issued out of band (Terraform output for BYOC; internally for
// managed) and must be presented on the first handshake.
//
// Only the SHA-256 of the token is stored — a database leak must not yield
// usable join tokens.
type DaemonJoinToken struct {
	// TokenHash is hex(sha256(token)). The plaintext is returned once, at
	// creation, and never persisted.
	TokenHash string    `gorm:"primaryKey" json:"token_hash"`
	OrgID     string    `gorm:"index;not null" json:"org_id"`
	ExpiresAt time.Time `gorm:"not null" json:"expires_at"`
	// UsedAt is stamped when the token is redeemed, making it single-use.
	UsedAt    *time.Time `json:"used_at"`
	CreatedAt time.Time  `gorm:"not null;default:current_timestamp" json:"created_at"`
}

func (DaemonJoinToken) TableName() string { return "daemon_join_tokens" }
