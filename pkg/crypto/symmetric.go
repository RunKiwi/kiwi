package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
)

// devEncryptionKey is used only when KIWI_ENCRYPTION_KEY is unset (local dev/tests).
const devEncryptionKey = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

// masterKey resolves the 32-byte AES master key from KIWI_ENCRYPTION_KEY
// (hex-encoded), falling back to a fixed dev key when unset.
func masterKey() ([]byte, error) {
	s := os.Getenv("KIWI_ENCRYPTION_KEY")
	if s == "" {
		s = devEncryptionKey
	}
	k, err := hex.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("invalid KIWI_ENCRYPTION_KEY (must be 32-byte hex): %w", err)
	}
	if len(k) != 32 {
		return nil, errors.New("KIWI_ENCRYPTION_KEY must be exactly 32 bytes")
	}
	return k, nil
}

// EncryptAtRest encrypts plaintext with AES-256-GCM under the master key,
// returning hex(nonce || ciphertext). Used for secrets persisted in the SaaS DB.
func EncryptAtRest(plaintext string) (string, error) {
	key, err := masterKey()
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return hex.EncodeToString(ciphertext), nil
}

// DecryptAtRest reverses EncryptAtRest.
func DecryptAtRest(ciphertextHex string) (string, error) {
	if ciphertextHex == "" {
		return "", nil
	}
	key, err := masterKey()
	if err != nil {
		return "", err
	}
	raw, err := hex.DecodeString(ciphertextHex)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(raw) < gcm.NonceSize() {
		return "", errors.New("ciphertext too short")
	}
	nonce, ct := raw[:gcm.NonceSize()], raw[gcm.NonceSize():]
	plaintext, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return "", err
	}
	return string(plaintext), nil
}
