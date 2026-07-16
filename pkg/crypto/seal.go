package crypto

import (
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"errors"

	"golang.org/x/crypto/nacl/box"
)

// SealToPublicKey encrypts plaintext to an X25519 public key using a NaCl
// anonymous sealed box (an ephemeral sender keypair is generated per call, so
// there is no sender identity). Only the holder of the matching X25519 private
// key can open it. Returns base64(ciphertext).
//
// This is the "encrypt to the daemon's public key" primitive: the Control Plane
// seals customer credentials to a daemon's registered public key so they can be
// carried to the customer VPC without the SaaS transport ever seeing plaintext.
func SealToPublicKey(pub *ecdh.PublicKey, plaintext []byte) (string, error) {
	pb := pub.Bytes()
	if len(pb) != 32 {
		return "", errors.New("invalid X25519 public key length")
	}
	var recipient [32]byte
	copy(recipient[:], pb)

	sealed, err := box.SealAnonymous(nil, plaintext, &recipient, rand.Reader)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(sealed), nil
}

// OpenSealed decrypts a base64 sealed box produced by SealToPublicKey using the
// recipient's X25519 private key. It fails if the ciphertext was sealed to a
// different key or has been tampered with.
func OpenSealed(priv *ecdh.PrivateKey, b64 string) ([]byte, error) {
	sealed, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return nil, err
	}

	privBytes := priv.Bytes()
	pubBytes := priv.PublicKey().Bytes()
	if len(privBytes) != 32 || len(pubBytes) != 32 {
		return nil, errors.New("invalid X25519 key length")
	}
	var pk, pub [32]byte
	copy(pk[:], privBytes)
	copy(pub[:], pubBytes)

	out, ok := box.OpenAnonymous(nil, sealed, &pub, &pk)
	if !ok {
		return nil, errors.New("failed to open sealed box (wrong key or corrupt data)")
	}
	return out, nil
}
