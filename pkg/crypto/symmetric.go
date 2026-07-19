package crypto

import (
	"context"
)

// devEncryptionKey is used only when KIWI_ENCRYPTION_KEY is unset (local dev/tests).
const devEncryptionKey = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

// EncryptAtRest encrypts plaintext using the configured KeyManager.
// It returns a hex string representing the ciphertext (and envelope metadata if KMS is used).
func EncryptAtRest(plaintext string) (string, error) {
	km, err := GetKeyManager(context.Background())
	if err != nil {
		return "", err
	}
	return km.Encrypt(context.Background(), plaintext)
}

// DecryptAtRest reverses EncryptAtRest.
func DecryptAtRest(ciphertextHex string) (string, error) {
	km, err := GetKeyManager(context.Background())
	if err != nil {
		return "", err
	}
	return km.Decrypt(context.Background(), ciphertextHex)
}
