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

// signBytes and verifyBytes are the ONLY places this package produces or checks
// a signature. Every record type routes through them, so a second record type
// cannot quietly adopt a different construction — signing a digest instead of
// the message, say, which would still be labelled "ed25519" and would silently
// fail for any verifier that guessed the other one.
func signBytes(b []byte, keyID string, priv ed25519.PrivateKey) *Signature {
	return &Signature{
		Alg: "ed25519",
		Key: keyID,
		Sig: base64.StdEncoding.EncodeToString(crypto.Sign(priv, b)),
	}
}

func verifyBytes(b []byte, sig *Signature, pub ed25519.PublicKey) error {
	if sig == nil {
		return errors.New("ver: missing signature")
	}
	if len(pub) == 0 {
		return errors.New("ver: no public key")
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
	return signBytes(b, keyID, priv), nil
}

// VerifyRecord checks a signature over the record's canonical signing payload.
func VerifyRecord(rec *Record, sig *Signature, pub ed25519.PublicKey) error {
	b, err := signingPayload(rec)
	if err != nil {
		return err
	}
	return verifyBytes(b, sig, pub)
}

// mergeSigningPayload is the merge record's equivalent of signingPayload: the
// record with its signature cleared. Same invariant, same reason — attaching a
// signature must not change what the signature covers, or what was chained.
func mergeSigningPayload(rec *MergeRecord) ([]byte, error) {
	if rec == nil {
		return nil, errors.New("ver: nil merge record")
	}
	clone := *rec
	clone.RecordSignature = nil
	return Canonicalize(&clone)
}

// MergeRecordHash is the chain link for a merge record. It uses the same
// "sha256:<hex>" form as RecordHash, because a merge record's hash becomes the
// next record's prev_record_hash and the chain must not mix formats.
func MergeRecordHash(rec *MergeRecord) (string, error) {
	b, err := mergeSigningPayload(rec)
	if err != nil {
		return "", err
	}
	return hashBytes(b), nil
}

// SignMergeRecord counter-signs a merge record and marks it attested, mirroring
// SignRecord exactly.
func SignMergeRecord(rec *MergeRecord, keyID string, priv ed25519.PrivateKey) (*Signature, error) {
	if len(priv) == 0 {
		return nil, ErrNoSigningKey
	}
	rec.Attestation = AttestationSigned
	b, err := mergeSigningPayload(rec)
	if err != nil {
		return nil, err
	}
	return signBytes(b, keyID, priv), nil
}

// VerifyMergeRecord checks a merge record's signature over its canonical
// signing payload.
func VerifyMergeRecord(rec *MergeRecord, sig *Signature, pub ed25519.PublicKey) error {
	b, err := mergeSigningPayload(rec)
	if err != nil {
		return err
	}
	return verifyBytes(b, sig, pub)
}

// postMergeSigningPayload is the post-merge verification record's equivalent
// of signingPayload: the record with its signature cleared. Same invariant,
// same reason — attaching a signature must not change what the signature
// covers, or what was chained.
func postMergeSigningPayload(rec *PostMergeVerificationRecord) ([]byte, error) {
	if rec == nil {
		return nil, errors.New("ver: nil postmerge verification record")
	}
	clone := *rec
	clone.RecordSignature = nil
	return Canonicalize(&clone)
}

// PostMergeVerificationRecordHash hashes rec's canonical form (signature
// fields excluded), for chaining — same shape as MergeRecordHash.
func PostMergeVerificationRecordHash(rec *PostMergeVerificationRecord) (string, error) {
	b, err := postMergeSigningPayload(rec)
	if err != nil {
		return "", err
	}
	return hashBytes(b), nil
}

// SignPostMergeVerificationRecord signs rec, setting its Attestation to
// "signed" as a side effect — same contract as SignMergeRecord.
func SignPostMergeVerificationRecord(rec *PostMergeVerificationRecord, keyID string, priv ed25519.PrivateKey) (*Signature, error) {
	if len(priv) == 0 {
		return nil, ErrNoSigningKey
	}
	rec.Attestation = AttestationSigned
	b, err := postMergeSigningPayload(rec)
	if err != nil {
		return nil, err
	}
	return signBytes(b, keyID, priv), nil
}

// VerifyPostMergeVerificationRecord checks sig against rec's current content
// — a tampered Verdict or Evidence after signing will fail this exactly as
// VerifyMergeRecord catches a tampered MergeCommit.
func VerifyPostMergeVerificationRecord(rec *PostMergeVerificationRecord, sig *Signature, pub ed25519.PublicKey) error {
	b, err := postMergeSigningPayload(rec)
	if err != nil {
		return err
	}
	return verifyBytes(b, sig, pub)
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
	return signBytes(b, keyID, priv), nil
}

// VerifyExecution re-derives the attestation payload and checks the daemon's
// signature over it.
func VerifyExecution(pub ed25519.PublicKey, sig *Signature, taskID, status string, events []TaskEvent) error {
	b, err := Canonicalize(ExecutionAttestation{TaskID: taskID, Status: status, Events: events})
	if err != nil {
		return err
	}
	return verifyBytes(b, sig, pub)
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

// ParseSigningKey builds a signing identity from a base64 Ed25519 seed (32
// bytes) or full private key (64 bytes). Exported and pure so a caller — or a
// test in another package — can construct a key without going through process
// environment or the memoized global.
func ParseSigningKey(raw, id string) (*SigningKey, error) {
	if raw == "" {
		return nil, ErrNoSigningKey
	}
	b, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		return nil, fmt.Errorf("ver: signing key is not valid base64: %w", err)
	}
	var priv ed25519.PrivateKey
	switch len(b) {
	case ed25519.SeedSize:
		priv = ed25519.NewKeyFromSeed(b)
	case ed25519.PrivateKeySize:
		priv = ed25519.PrivateKey(b)
	default:
		return nil, fmt.Errorf("ver: signing key must decode to %d or %d bytes, got %d",
			ed25519.SeedSize, ed25519.PrivateKeySize, len(b))
	}
	if id == "" {
		id = "cp-default"
	}
	return &SigningKey{ID: id, Priv: priv, Pub: priv.Public().(ed25519.PublicKey)}, nil
}

// CPSigningKey loads the Control Plane signing key from the environment, once.
//
// KIWI_VER_SIGNING_KEY is a base64 Ed25519 seed (32 bytes) or full private key
// (64 bytes); KIWI_VER_SIGNING_KEY_ID names it so a record stays verifiable
// across a future rotation. When unset, this returns ErrNoSigningKey and the
// caller must persist the record unsigned rather than inventing a key.
//
// There is deliberately no reset: production code must not be able to swap the
// signing identity at runtime. Tests inject a key instead of mutating this.
func CPSigningKey() (*SigningKey, error) {
	cpKeyOnce.Do(func() {
		cpKey, cpKeyErr = ParseSigningKey(
			os.Getenv("KIWI_VER_SIGNING_KEY"),
			os.Getenv("KIWI_VER_SIGNING_KEY_ID"),
		)
	})
	if cpKeyErr != nil {
		return nil, cpKeyErr
	}
	return cpKey, nil
}
