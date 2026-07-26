package ver

import (
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"sync"

	"github.com/ibreakthecloud/kiwi/pkg/crypto"
)

// ErrNoSigningKey reports that no Control Plane signing key is configured, so
// records cannot be counter-signed. It is deliberately an error rather than a
// silent fallback: a self-generated key would make every record unverifiable
// the moment the process restarts, which is worse than an unsigned record
// because the signature would assert an authenticity nothing can check.
var ErrNoSigningKey = errors.New("ver: no Control Plane signing key configured (set KIWI_VER_SIGNING_KEY)")

// signingPayload is the exact byte sequence that both Sign and Verify operate
// on: the record with BOTH signature fields cleared, canonicalized per RFC 8785.
//
// This is the single most important invariant in the package. Signing one form
// and hashing another leaves a record that nothing can verify, so every path
// that produces or checks a signature must go through here.
func signingPayload(rec *Record) ([]byte, error) {
	if rec == nil {
		return nil, errors.New("ver: nil record")
	}
	clone := *rec
	clone.ExecSignature = nil
	clone.RecordSignature = nil
	return Canonicalize(&clone)
}

// SigningPayload exposes the canonical signing bytes so a daemon (which signs
// the execution subtree) and an offline verifier can derive them identically.
func SigningPayload(rec *Record) ([]byte, error) { return signingPayload(rec) }

// RecordHash is the chain link for a record: the hash of its signing payload.
// Because it excludes the signatures, appending a signature never changes a
// record's identity, and the hash a verifier recomputes always matches the one
// that was chained.
func RecordHash(rec *Record) (string, error) {
	b, err := signingPayload(rec)
	if err != nil {
		return "", err
	}
	return hashBytes(b), nil
}

// SignRecord counter-signs a record with the Control Plane key and marks it
// attested, in that order.
//
// Setting Attestation is deliberately this function's job rather than the
// caller's. Attestation is inside the signing payload, so a caller that set it
// after signing would silently invalidate the signature — the exact failure
// this package exists to prevent. Doing both here makes that unrepresentable.
func SignRecord(rec *Record, keyID string, priv ed25519.PrivateKey) (*Signature, error) {
	if len(priv) == 0 {
		return nil, ErrNoSigningKey
	}
	rec.Attestation = AttestationSigned
	b, err := signingPayload(rec)
	if err != nil {
		return nil, err
	}
	return &Signature{
		Alg: "ed25519",
		Key: keyID,
		Sig: base64.StdEncoding.EncodeToString(crypto.Sign(priv, b)),
	}, nil
}

// VerifyRecord checks a signature over the record's canonical signing payload.
func VerifyRecord(rec *Record, sig *Signature, pub ed25519.PublicKey) error {
	if sig == nil {
		return errors.New("ver: missing signature")
	}
	if len(pub) == 0 {
		return errors.New("ver: no public key")
	}
	b, err := signingPayload(rec)
	if err != nil {
		return err
	}
	raw, err := base64.StdEncoding.DecodeString(sig.Sig)
	if err != nil {
		return errors.New("ver: malformed base64 signature")
	}
	if !crypto.Verify(pub, b, raw) {
		return errors.New("ver: invalid signature")
	}
	return nil
}

// ExecutionAttestation is the exact payload a daemon signs when it reports a
// task. Both sides construct it from the same fields in the same order, so the
// Control Plane can re-derive the bytes and check the signature without
// trusting anything the daemon said about them.
type ExecutionAttestation struct {
	TaskID string      `json:"task_id"`
	Status string      `json:"status"`
	Events []TaskEvent `json:"events"`
}

// SignExecution attests a task's telemetry with the daemon's Ed25519 key.
func SignExecution(priv ed25519.PrivateKey, keyID, taskID, status string, events []TaskEvent) (*Signature, error) {
	if len(priv) == 0 {
		return nil, errors.New("ver: no daemon signing key")
	}
	b, err := Canonicalize(ExecutionAttestation{TaskID: taskID, Status: status, Events: events})
	if err != nil {
		return nil, err
	}
	return &Signature{
		Alg: "ed25519",
		Key: keyID,
		Sig: base64.StdEncoding.EncodeToString(crypto.Sign(priv, b)),
	}, nil
}

// VerifyExecution re-derives the attestation payload and checks the daemon's
// signature over it.
func VerifyExecution(pub ed25519.PublicKey, sig *Signature, taskID, status string, events []TaskEvent) error {
	if sig == nil {
		return errors.New("ver: missing execution signature")
	}
	if len(pub) == 0 {
		return errors.New("ver: no daemon public key")
	}
	b, err := Canonicalize(ExecutionAttestation{TaskID: taskID, Status: status, Events: events})
	if err != nil {
		return err
	}
	raw, err := base64.StdEncoding.DecodeString(sig.Sig)
	if err != nil {
		return errors.New("ver: malformed base64 signature")
	}
	if !crypto.Verify(pub, b, raw) {
		return errors.New("ver: invalid execution signature")
	}
	return nil
}

// SigningKey is the Control Plane's configured signing identity.
type SigningKey struct {
	ID   string
	Priv ed25519.PrivateKey
	Pub  ed25519.PublicKey
}

var (
	cpKeyOnce sync.Once
	cpKey     *SigningKey
	cpKeyErr  error
)

// CPSigningKey loads the Control Plane signing key from the environment, once.
//
// KIWI_VER_SIGNING_KEY is a base64 Ed25519 seed (32 bytes) or full private key
// (64 bytes); KIWI_VER_SIGNING_KEY_ID names it so a record stays verifiable
// across a future rotation. When unset, this returns ErrNoSigningKey and the
// caller must persist the record unsigned rather than inventing a key.
func CPSigningKey() (*SigningKey, error) {
	cpKeyOnce.Do(func() {
		raw := os.Getenv("KIWI_VER_SIGNING_KEY")
		if raw == "" {
			cpKeyErr = ErrNoSigningKey
			return
		}
		b, err := base64.StdEncoding.DecodeString(raw)
		if err != nil {
			cpKeyErr = fmt.Errorf("ver: KIWI_VER_SIGNING_KEY is not valid base64: %w", err)
			return
		}
		var priv ed25519.PrivateKey
		switch len(b) {
		case ed25519.SeedSize:
			priv = ed25519.NewKeyFromSeed(b)
		case ed25519.PrivateKeySize:
			priv = ed25519.PrivateKey(b)
		default:
			cpKeyErr = fmt.Errorf("ver: KIWI_VER_SIGNING_KEY must decode to %d or %d bytes, got %d",
				ed25519.SeedSize, ed25519.PrivateKeySize, len(b))
			return
		}
		id := os.Getenv("KIWI_VER_SIGNING_KEY_ID")
		if id == "" {
			id = "cp-default"
		}
		cpKey = &SigningKey{ID: id, Priv: priv, Pub: priv.Public().(ed25519.PublicKey)}
	})
	if cpKeyErr != nil {
		return nil, cpKeyErr
	}
	return cpKey, nil
}
