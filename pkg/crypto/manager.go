package crypto

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"

	kms "cloud.google.com/go/kms/apiv1"
	"cloud.google.com/go/kms/apiv1/kmspb"
)

// KeyManager abstracts at-rest encryption and decryption.
type KeyManager interface {
	Encrypt(ctx context.Context, plaintext string) (string, error)
	Decrypt(ctx context.Context, ciphertextHex string) (string, error)
}

var (
	globalManager KeyManager
	managerOnce   sync.Once
	managerErr    error
)

// GetKeyManager returns the configured KeyManager.
func GetKeyManager(ctx context.Context) (KeyManager, error) {
	managerOnce.Do(func() {
		kmsKey := os.Getenv("KIWI_KMS_KEY")
		if kmsKey != "" {
			globalManager, managerErr = NewKMSKeyManager(ctx, kmsKey)
		} else {
			globalManager, managerErr = NewEnvKeyManager()
		}
	})
	return globalManager, managerErr
}

// SetKeyManagerForTest allows injecting a mock in tests.
func SetKeyManagerForTest(m KeyManager) {
	globalManager = m
}

// ResetKeyManagerForTest resets the singleton.
func ResetKeyManagerForTest() {
	managerOnce = sync.Once{}
	globalManager = nil
	managerErr = nil
}

// EnvKeyManager implements legacy AES-GCM with a static environment key.
type EnvKeyManager struct {
	key []byte
}

func NewEnvKeyManager() (*EnvKeyManager, error) {
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
	return &EnvKeyManager{key: k}, nil
}

func (m *EnvKeyManager) Encrypt(_ context.Context, plaintext string) (string, error) {
	block, err := aes.NewCipher(m.key)
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

func (m *EnvKeyManager) Decrypt(_ context.Context, ciphertextHex string) (string, error) {
	if ciphertextHex == "" {
		return "", nil
	}
	// Check for KMS prefix, just in case
	if strings.HasPrefix(ciphertextHex, "kms:v1:") {
		return "", errors.New("cannot decrypt KMS ciphertext with EnvKeyManager")
	}
	raw, err := hex.DecodeString(ciphertextHex)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(m.key)
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

// KMSKeyManager implements envelope encryption using Cloud KMS.
type KMSKeyManager struct {
	client *kms.KeyManagementClient
	kmsKey string
}

func NewKMSKeyManager(ctx context.Context, kmsKey string) (*KMSKeyManager, error) {
	client, err := kms.NewKeyManagementClient(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to create KMS client: %w", err)
	}
	return &KMSKeyManager{
		client: client,
		kmsKey: kmsKey,
	}, nil
}

func (m *KMSKeyManager) Encrypt(ctx context.Context, plaintext string) (string, error) {
	// 1. Generate DEK
	dek := make([]byte, 32)
	if _, err := rand.Read(dek); err != nil {
		return "", err
	}

	// 2. Encrypt DEK with KMS
	req := &kmspb.EncryptRequest{
		Name:      m.kmsKey,
		Plaintext: dek,
	}
	resp, err := m.client.Encrypt(ctx, req)
	if err != nil {
		return "", fmt.Errorf("failed to wrap DEK: %w", err)
	}
	wrappedDEK := resp.Ciphertext

	// 3. Encrypt payload with DEK
	block, err := aes.NewCipher(dek)
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

	// Format: kms:v1:<wrappedDEKHex>:<payloadCiphertextHex>
	return fmt.Sprintf("kms:v1:%s:%s", hex.EncodeToString(wrappedDEK), hex.EncodeToString(ciphertext)), nil
}

func (m *KMSKeyManager) Decrypt(ctx context.Context, ciphertextStr string) (string, error) {
	if ciphertextStr == "" {
		return "", nil
	}

	// Fallback for legacy format
	if !strings.HasPrefix(ciphertextStr, "kms:v1:") {
		// Try to decrypt using EnvKeyManager for migration/fallback
		fallback, err := NewEnvKeyManager()
		if err != nil {
			return "", fmt.Errorf("failed to instantiate EnvKeyManager for legacy decrypt: %w", err)
		}
		return fallback.Decrypt(ctx, ciphertextStr)
	}

	parts := strings.Split(ciphertextStr, ":")
	if len(parts) != 4 {
		return "", errors.New("invalid KMS ciphertext format")
	}

	wrappedDEKHex := parts[2]
	payloadCiphertextHex := parts[3]

	wrappedDEK, err := hex.DecodeString(wrappedDEKHex)
	if err != nil {
		return "", err
	}

	// 1. Decrypt DEK with KMS
	// The Name field must be the resource name of the CryptoKey or CryptoKeyVersion used for encryption.
	// Cloud KMS determines the correct version from the ciphertext payload.
	req := &kmspb.DecryptRequest{
		Name:       m.kmsKey,
		Ciphertext: wrappedDEK,
	}
	resp, err := m.client.Decrypt(ctx, req)
	if err != nil {
		return "", fmt.Errorf("failed to unwrap DEK: %w", err)
	}
	dek := resp.Plaintext

	// 2. Decrypt payload with DEK
	payloadRaw, err := hex.DecodeString(payloadCiphertextHex)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(dek)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(payloadRaw) < gcm.NonceSize() {
		return "", errors.New("payload ciphertext too short")
	}
	nonce, ct := payloadRaw[:gcm.NonceSize()], payloadRaw[gcm.NonceSize():]
	plaintext, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return "", err
	}

	return string(plaintext), nil
}
