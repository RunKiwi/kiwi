package crypto

import (
	"bytes"
	"testing"
)

func TestGenerateAndEncodeDecodeKeys(t *testing.T) {
	// 1. Generate Key Pair
	pub, priv, err := GenerateKeyPair()
	if err != nil {
		t.Fatalf("failed to generate key pair: %v", err)
	}

	if pub == nil || priv == nil {
		t.Fatal("generated keys should not be nil")
	}

	// 2. Encode Private Key
	privPEM, err := EncodePrivateKeyToPEM(priv)
	if err != nil {
		t.Fatalf("failed to encode private key: %v", err)
	}

	// 3. Decode Private Key
	decodedPriv, err := DecodePrivateKeyFromPEM(privPEM)
	if err != nil {
		t.Fatalf("failed to decode private key: %v", err)
	}

	if !bytes.Equal(priv.Bytes(), decodedPriv.Bytes()) {
		t.Errorf("decoded private key does not match original")
	}

	// 4. Encode Public Key
	pubPEM, err := EncodePublicKeyToPEM(pub)
	if err != nil {
		t.Fatalf("failed to encode public key: %v", err)
	}

	// 5. Decode Public Key
	decodedPub, err := DecodePublicKeyFromPEM(pubPEM)
	if err != nil {
		t.Fatalf("failed to decode public key: %v", err)
	}

	if !bytes.Equal(pub.Bytes(), decodedPub.Bytes()) {
		t.Errorf("decoded public key does not match original")
	}
}

func TestDecodeInvalidPEM(t *testing.T) {
	_, err := DecodePrivateKeyFromPEM([]byte("invalid pem data"))
	if err == nil {
		t.Error("expected error decoding invalid private key PEM, got nil")
	}

	_, err = DecodePublicKeyFromPEM([]byte("invalid pem data"))
	if err == nil {
		t.Error("expected error decoding invalid public key PEM, got nil")
	}
}
