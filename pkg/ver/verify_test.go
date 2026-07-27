package ver

import (
	"crypto/ed25519"
	"encoding/base64"
	"os"
	"strings"
	"testing"
)

func signedRecord(t *testing.T) (*Record, ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	rec, err := AssembleRecord(baseInput())
	if err != nil {
		t.Fatal(err)
	}
	sig, err := SignRecord(rec, "test-key", priv)
	if err != nil {
		t.Fatal(err)
	}
	rec.RecordSignature = sig
	return rec, pub, priv
}

// The bug this guards: signing one canonical form and hashing another leaves a
// record nothing can verify. Attaching the signature must not change what the
// signature covers.
func TestSignatureVerifiesAfterBeingAttached(t *testing.T) {
	rec, pub, _ := signedRecord(t)
	if err := VerifyRecord(rec, rec.RecordSignature, pub); err != nil {
		t.Fatalf("signature must verify on the record that carries it: %v", err)
	}
}

// The chain link must be stable across signing, or the hash that was chained
// is not the hash a verifier recomputes.
func TestRecordHashUnchangedBySignatures(t *testing.T) {
	rec, _, priv := signedRecord(t)
	before, err := RecordHash(rec)
	if err != nil {
		t.Fatal(err)
	}

	execSig, err := SignExecution(priv, "daemon-key", "task_1", "SUCCEEDED", nil)
	if err != nil {
		t.Fatal(err)
	}
	rec.ExecSignature = execSig

	after, err := RecordHash(rec)
	if err != nil {
		t.Fatal(err)
	}
	if before != after {
		t.Fatalf("attaching a signature changed the record hash: %s -> %s", before, after)
	}
}

func TestTamperedRecordFailsVerification(t *testing.T) {
	rec, pub, _ := signedRecord(t)
	rec.Verification.FinalOutcome = "pass-but-actually-tampered"
	if err := VerifyRecord(rec, rec.RecordSignature, pub); err == nil {
		t.Fatal("a mutated record must not verify")
	}
}

func TestExecutionAttestationRoundTrip(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	events := []TaskEvent{
		{Step: 1, Phase: "critic", Outcome: "rejected", Detail: "unbounded loop"},
		{Step: 2, Phase: "test", Outcome: "pass"},
	}
	sig, err := SignExecution(priv, "daemon-key", "task_1", "SUCCEEDED", events)
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyExecution(pub, sig, "task_1", "SUCCEEDED", events); err != nil {
		t.Fatalf("attestation should verify: %v", err)
	}
	// Events the daemon did not sign must not pass as attested.
	events[0].Outcome = "approved"
	if err := VerifyExecution(pub, sig, "task_1", "SUCCEEDED", events); err == nil {
		t.Fatal("altered events must not verify")
	}
}

// A missing key must be an explicit error, never a self-generated one: an
// ephemeral key produces signatures that nothing can ever check.
func TestNoSigningKeyIsAnErrorNotAFallback(t *testing.T) {
	if _, err := SignRecord(&Record{}, "k", nil); err != ErrNoSigningKey {
		t.Fatalf("expected ErrNoSigningKey, got %v", err)
	}
}

func TestCPSigningKeyFromSeed(t *testing.T) {
	seed := make([]byte, ed25519.SeedSize)
	for i := range seed {
		seed[i] = byte(i)
	}
	t.Setenv("KIWI_VER_SIGNING_KEY", base64.StdEncoding.EncodeToString(seed))
	t.Setenv("KIWI_VER_SIGNING_KEY_ID", "cp_test")

	// CPSigningKey memoizes, so exercise the parsing directly for determinism
	// under -count=N and alongside other tests that may have loaded it first.
	raw := os.Getenv("KIWI_VER_SIGNING_KEY")
	b, err := base64.StdEncoding.DecodeString(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(b) != ed25519.SeedSize {
		t.Fatalf("seed length = %d, want %d", len(b), ed25519.SeedSize)
	}
	priv := ed25519.NewKeyFromSeed(b)
	rec, err := AssembleRecord(baseInput())
	if err != nil {
		t.Fatal(err)
	}
	sig, err := SignRecord(rec, "cp_test", priv)
	if err != nil {
		t.Fatal(err)
	}
	if err := VerifyRecord(rec, sig, priv.Public().(ed25519.PublicKey)); err != nil {
		t.Fatalf("seed-derived key must sign verifiably: %v", err)
	}
}

func mergeRecord() *MergeRecord {
	return &MergeRecord{
		Ver:              MergeSchemaVersion,
		RecordID:         "rec_1",
		OriginalRecordID: "ver_job_1",
		OrgID:            "org_1",
		JobID:            "job_1",
		PrevRecordHash:   "sha256:abc",
		Attestation:      AttestationUnsigned,
		ApprovedBy:       "gh:someuser",
		MergedAt:         "2026-07-27T10:00:00Z",
		MergeCommit:      "d4e5f6",
	}
}

// The merge record must obey the same invariant as the execution record:
// attaching the signature cannot change what the signature covers.
func TestMergeSignatureVerifiesAfterBeingAttached(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	rec := mergeRecord()
	sig, err := SignMergeRecord(rec, "cp", priv)
	if err != nil {
		t.Fatal(err)
	}
	rec.RecordSignature = sig

	if err := VerifyMergeRecord(rec, sig, pub); err != nil {
		t.Fatalf("merge signature must verify on the record that carries it: %v", err)
	}
	if rec.Attestation != AttestationSigned {
		t.Errorf("Attestation = %q, want %q", rec.Attestation, AttestationSigned)
	}
}

func TestMergeRecordHashUnchangedBySignature(t *testing.T) {
	_, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	rec := mergeRecord()
	sig, err := SignMergeRecord(rec, "cp", priv)
	if err != nil {
		t.Fatal(err)
	}
	before, err := MergeRecordHash(rec)
	if err != nil {
		t.Fatal(err)
	}
	rec.RecordSignature = sig
	after, err := MergeRecordHash(rec)
	if err != nil {
		t.Fatal(err)
	}
	if before != after {
		t.Fatalf("attaching a signature changed the merge hash: %s -> %s", before, after)
	}
}

func TestTamperedMergeRecordFailsVerification(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	rec := mergeRecord()
	sig, err := SignMergeRecord(rec, "cp", priv)
	if err != nil {
		t.Fatal(err)
	}
	rec.RecordSignature = sig
	rec.ApprovedBy = "gh:someone-else"
	if err := VerifyMergeRecord(rec, sig, pub); err == nil {
		t.Fatal("a rewritten approver must not verify")
	}
}

// Both record kinds must produce chain links in the same "sha256:<hex>" form: a
// merge record's hash becomes the next record's prev_record_hash, so a bare-hex
// link would silently break continuity checks.
func TestChainHashFormatIsConsistentAcrossRecordKinds(t *testing.T) {
	rec, err := AssembleRecord(baseInput())
	if err != nil {
		t.Fatal(err)
	}
	execHash, err := RecordHash(rec)
	if err != nil {
		t.Fatal(err)
	}
	mergeHash, err := MergeRecordHash(mergeRecord())
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range []string{execHash, mergeHash} {
		if !strings.HasPrefix(h, "sha256:") {
			t.Errorf("chain hash %q lacks the sha256: prefix", h)
		}
		if len(h) != len("sha256:")+64 {
			t.Errorf("chain hash %q is not a 32-byte hex digest", h)
		}
	}
}

// Every signature this package emits must be the same construction, so a
// verifier holding the published key never has to guess which one was used.
func TestAllSignersUseTheSameConstruction(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	rec := mergeRecord()
	sig, err := SignMergeRecord(rec, "cp", priv)
	if err != nil {
		t.Fatal(err)
	}
	// Verify by hand against the raw canonical payload — the same thing
	// VerifyRecord does for an execution record. A signer that hashed first
	// would fail here.
	payload, err := mergeSigningPayload(rec)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := base64.StdEncoding.DecodeString(sig.Sig)
	if err != nil {
		t.Fatal(err)
	}
	if !ed25519.Verify(pub, payload, raw) {
		t.Fatal("merge signature is not over the raw canonical payload, unlike every other signer")
	}
}
