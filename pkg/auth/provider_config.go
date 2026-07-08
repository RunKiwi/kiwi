package auth

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"

	"gorm.io/gorm"
)

// OrgProviderConfig represents the organization's customized LLM provider config.
type OrgProviderConfig struct {
	OrgID           string `json:"org_id" gorm:"primaryKey;index;not null"`
	ProviderName    string `json:"provider_name"` // e.g. "anthropic"
	EncryptedAPIKey string `json:"-"`
	ActorModel      string `json:"actor_model"`
	CriticModel     string `json:"critic_model"`
}

// TableName overrides the default GORM table name.
func (OrgProviderConfig) TableName() string { return "org_provider_configs" }

// getEncryptionKey retrieves the 32-byte master key from the environment.
// Falls back to a default key for local development if not set.
func getEncryptionKey() ([]byte, error) {
	keyStr := os.Getenv("KIWI_ENCRYPTION_KEY")
	if keyStr == "" {
		// Fallback developer key (32 bytes hex encoded = 64 characters)
		keyStr = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	}
	key, err := hex.DecodeString(keyStr)
	if err != nil {
		return nil, fmt.Errorf("invalid KIWI_ENCRYPTION_KEY format (must be 32-byte hex string): %w", err)
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("invalid KIWI_ENCRYPTION_KEY length (must be exactly 32 bytes)")
	}
	return key, nil
}

// EncryptKey encrypts a plaintext API key using AES-256-GCM.
func EncryptKey(plaintext string) (string, error) {
	masterKey, err := getEncryptionKey()
	if err != nil {
		return "", err
	}

	block, err := aes.NewCipher(masterKey)
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

// DecryptKey decrypts the AES-256-GCM encrypted API key.
func (c *OrgProviderConfig) DecryptKey() (string, error) {
	if c.EncryptedAPIKey == "" {
		return "", nil
	}

	masterKey, err := getEncryptionKey()
	if err != nil {
		return "", err
	}

	ciphertext, err := hex.DecodeString(c.EncryptedAPIKey)
	if err != nil {
		return "", err
	}

	block, err := aes.NewCipher(masterKey)
	if err != nil {
		return "", err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	nonceSize := gcm.NonceSize()
	if len(ciphertext) < nonceSize {
		return "", errors.New("ciphertext too short")
	}

	nonce, actualCiphertext := ciphertext[:nonceSize], ciphertext[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, actualCiphertext, nil)
	if err != nil {
		return "", err
	}

	return string(plaintext), nil
}

// GetProviderConfig retrieves config for an org from the DB.
func GetProviderConfig(db *gorm.DB, orgID string) (*OrgProviderConfig, error) {
	var config OrgProviderConfig
	if err := db.Where("org_id = ?", orgID).First(&config).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil // No custom config
		}
		return nil, err
	}
	return &config, nil
}
