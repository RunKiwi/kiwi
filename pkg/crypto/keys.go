package crypto

import (
	"crypto/ecdh"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"errors"
)

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
