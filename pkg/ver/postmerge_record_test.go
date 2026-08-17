package ver

import (
	"crypto/ed25519"
	"testing"
)

func generateTestKey() (ed25519.PublicKey, ed25519.PrivateKey, error) {
	return ed25519.GenerateKey(nil)
}

func TestPostMergeVerificationRecordSignAndVerify(t *testing.T) {
	pub, priv, err := generateTestKey()
	if err != nil {
		t.Fatal(err)
	}

	rec := &PostMergeVerificationRecord{
		Ver:              PostMergeVerifySchemaVersion,
		RecordID:         "rec-2",
		OriginalRecordID: "rec-1",
		OrgID:            "org1",
		JobID:            "job1",
		PrevRecordHash:   "sha256:abc",
		Verdict:          "VERIFIED",
		Evidence:         "24h window elapsed with no regression signal",
		FinalizedAt:      "2026-08-16T00:00:00Z",
	}

	sig, err := SignPostMergeVerificationRecord(rec, "test-key", priv)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	rec.RecordSignature = sig

	if err := VerifyPostMergeVerificationRecord(rec, sig, pub); err != nil {
		t.Errorf("verify: %v", err)
	}

	rec.Verdict = "REGRESSION" // tamper after signing
	if err := VerifyPostMergeVerificationRecord(rec, sig, pub); err == nil {
		t.Errorf("verify should fail after the record is tampered with")
	}
}

// A nil record must fail closed with an error, not panic — the same
// contract signingPayload and mergeSigningPayload guarantee for their
// record types.
func TestPostMergeVerificationRecordHashNilRecord(t *testing.T) {
	if _, err := PostMergeVerificationRecordHash(nil); err == nil {
		t.Fatal("expected an error for a nil record, got nil")
	}
}
