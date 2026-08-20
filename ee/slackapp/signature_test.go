// ee/slackapp/signature_test.go
package slackapp

import "testing"

func TestVerifySignatureAcceptsAValidV0Signature(t *testing.T) {
	secret := "shhh"
	ts := "1531420618"
	body := []byte(`{"type":"url_verification"}`)
	// Computed offline as HMAC-SHA256("shhh", "v0:1531420618:"+body), hex-encoded.
	sig := computeTestSig(t, secret, ts, body)
	if !VerifySignature(secret, ts, body, "v0="+sig) {
		t.Fatal("expected a correctly computed signature to verify")
	}
}

func TestVerifySignatureRejectsWrongSecret(t *testing.T) {
	ts := "1531420618"
	body := []byte(`{"type":"url_verification"}`)
	sig := computeTestSig(t, "shhh", ts, body)
	if VerifySignature("different-secret", ts, body, "v0="+sig) {
		t.Fatal("expected verification to fail with the wrong secret")
	}
}

func TestVerifySignatureRejectsMalformedHeader(t *testing.T) {
	if VerifySignature("shhh", "1531420618", []byte("{}"), "not-v0-prefixed") {
		t.Fatal("expected a malformed signature header to be rejected")
	}
}

func computeTestSig(t *testing.T, secret, ts string, body []byte) string {
	t.Helper()
	mac := hmacSHA256(secret, "v0:"+ts+":"+string(body))
	return mac
}
