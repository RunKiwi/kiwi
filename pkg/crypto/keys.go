package crypto

import (
	"crypto/ecdh"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"errors"
)

// PublicKeyFromRawBytes reconstructs an X25519 public key from its 32 raw
// bytes — the encoding a daemon sends over the wire as base64(pub.Bytes()).
// Distinct from DecodePublicKeyFromPEM, which handles the on-disk PEM form.
func PublicKeyFromRawBytes(raw []byte) (*ecdh.PublicKey, error) {
	return ecdh.X25519().NewPublicKey(raw)
}

// GenerateKeyPair generates a new X25519 public/private key pair.
func GenerateKeyPair() (*ecdh.PublicKey, *ecdh.PrivateKey, error) {
	priv, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	return priv.PublicKey(), priv, nil
}

// EncodePrivateKeyToPEM encodes an X25519 private key to PEM format.
func EncodePrivateKeyToPEM(priv *ecdh.PrivateKey) ([]byte, error) {
	b, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return nil, err
	}
	block := &pem.Block{
		Type:  "PRIVATE KEY",
		Bytes: b,
	}
	return pem.EncodeToMemory(block), nil
}

// DecodePrivateKeyFromPEM decodes an X25519 private key from PEM format.
func DecodePrivateKeyFromPEM(pemBytes []byte) (*ecdh.PrivateKey, error) {
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, errors.New("failed to decode PEM block containing private key")
	}

	priv, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, err
	}

	edPriv, ok := priv.(*ecdh.PrivateKey)
	if !ok {
		return nil, errors.New("not an X25519 private key")
	}

	return edPriv, nil
}

// EncodePublicKeyToPEM encodes an X25519 public key to PEM format.
func EncodePublicKeyToPEM(pub *ecdh.PublicKey) ([]byte, error) {
	b, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		return nil, err
	}
	block := &pem.Block{
		Type:  "PUBLIC KEY",
		Bytes: b,
	}
	return pem.EncodeToMemory(block), nil
}

// DecodePublicKeyFromPEM decodes an X25519 public key from PEM format.
func DecodePublicKeyFromPEM(pemBytes []byte) (*ecdh.PublicKey, error) {
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, errors.New("failed to decode PEM block containing public key")
	}

	pub, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, err
	}

	edPub, ok := pub.(*ecdh.PublicKey)
	if !ok {
		return nil, errors.New("not an X25519 public key")
	}

	return edPub, nil
}

// --- Ed25519 signing keys (daemon identity / request authentication) ---
//
// The X25519 keys above are used to *receive* credentials sealed to the daemon.
// The Ed25519 keys below are the daemon's signing identity: it signs each
// heartbeat so the Control Plane can authenticate the request against the
// registered public key (X25519 cannot sign, hence a separate key).

// GenerateSigningKeyPair generates a new Ed25519 signing key pair.
func GenerateSigningKeyPair() (ed25519.PublicKey, ed25519.PrivateKey, error) {
	return ed25519.GenerateKey(rand.Reader)
}

// Sign returns an Ed25519 signature over msg using priv.
func Sign(priv ed25519.PrivateKey, msg []byte) []byte {
	return ed25519.Sign(priv, msg)
}

// Verify reports whether sig is a valid Ed25519 signature of msg by pub.
func Verify(pub ed25519.PublicKey, msg, sig []byte) bool {
	return ed25519.Verify(pub, msg, sig)
}

// EncodeSigningPrivateKeyToPEM encodes an Ed25519 private key to PEM format.
func EncodeSigningPrivateKeyToPEM(priv ed25519.PrivateKey) ([]byte, error) {
	b, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return nil, err
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: b}), nil
}

// DecodeSigningPrivateKeyFromPEM decodes an Ed25519 private key from PEM format.
func DecodeSigningPrivateKeyFromPEM(pemBytes []byte) (ed25519.PrivateKey, error) {
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, errors.New("failed to decode PEM block containing signing private key")
	}
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	priv, ok := key.(ed25519.PrivateKey)
	if !ok {
		return nil, errors.New("not an Ed25519 private key")
	}
	return priv, nil
}
