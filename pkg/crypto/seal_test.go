package crypto

import (
	"bytes"
	"testing"
)

func TestSealOpenRoundTrip(t *testing.T) {
	pub, priv, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair: %v", err)
	}

	msg := []byte(`{"ANTHROPIC_API_KEY":"sk-ant-secret","GITHUB_TOKEN":"ghp_secret"}`)
	sealed, err := SealToPublicKey(pub, msg)
	if err != nil {
		t.Fatalf("SealToPublicKey: %v", err)
	}
	if sealed == "" || sealed == string(msg) {
		t.Fatal("sealed output must be non-empty ciphertext, not plaintext")
	}

	out, err := OpenSealed(priv, sealed)
	if err != nil {
		t.Fatalf("OpenSealed: %v", err)
	}
	if !bytes.Equal(out, msg) {
		t.Errorf("round-trip mismatch: got %q want %q", out, msg)
	}
}

func TestOpenSealedWrongKeyFails(t *testing.T) {
	pub, _, _ := GenerateKeyPair()
	_, otherPriv, _ := GenerateKeyPair()

	sealed, err := SealToPublicKey(pub, []byte("secret"))
	if err != nil {
		t.Fatalf("SealToPublicKey: %v", err)
	}
	if _, err := OpenSealed(otherPriv, sealed); err == nil {
		t.Error("opening with the wrong private key must fail")
	}
}

func TestOpenSealedTamperFails(t *testing.T) {
	pub, priv, _ := GenerateKeyPair()
	sealed, _ := SealToPublicKey(pub, []byte("secret"))
	// Corrupt the ciphertext.
	tampered := "A" + sealed[1:]
	if _, err := OpenSealed(priv, tampered); err == nil {
		t.Error("opening tampered ciphertext must fail")
	}
}

func TestEncryptDecryptAtRest(t *testing.T) {
	t.Setenv("KIWI_ENCRYPTION_KEY", "00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff")

	plaintext := "ghp_supersecrettoken"
	ct, err := EncryptAtRest(plaintext)
	if err != nil {
		t.Fatalf("EncryptAtRest: %v", err)
	}
	if ct == plaintext {
		t.Fatal("ciphertext must not equal plaintext")
	}
	got, err := DecryptAtRest(ct)
	if err != nil {
		t.Fatalf("DecryptAtRest: %v", err)
	}
	if got != plaintext {
		t.Errorf("round-trip mismatch: got %q want %q", got, plaintext)
	}

	// Two encryptions of the same plaintext differ (random nonce).
	ct2, _ := EncryptAtRest(plaintext)
	if ct == ct2 {
		t.Error("expected distinct ciphertexts due to random nonce")
	}
}
