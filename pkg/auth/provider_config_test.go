package auth

import (
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func setupProviderTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	if err := db.AutoMigrate(&OrgProviderConfig{}); err != nil {
		t.Fatalf("failed to migrate auth DB: %v", err)
	}
	return db
}

func TestEncryptDecryptKey(t *testing.T) {
	// Set custom master encryption key (32 bytes hex encoded = 64 characters)
	t.Setenv("KIWI_ENCRYPTION_KEY", "00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff")

	plaintext := "sk-ant-api03-abcdefghijkl"

	// 1. Encrypt key
	ciphertext, err := EncryptKey(plaintext)
	if err != nil {
		t.Fatalf("EncryptKey error: %v", err)
	}
	if ciphertext == plaintext {
		t.Errorf("ciphertext should not match plaintext")
	}

	// 2. Decrypt key
	config := OrgProviderConfig{
		OrgID:           "org-1",
		EncryptedAPIKey: ciphertext,
	}

	decrypted, err := config.DecryptKey()
	if err != nil {
		t.Fatalf("DecryptKey error: %v", err)
	}
	if decrypted != plaintext {
		t.Errorf("expected decrypted key %q, got %q", plaintext, decrypted)
	}
}

func TestOrgProviderConfigDBStorage(t *testing.T) {
	t.Setenv("KIWI_ENCRYPTION_KEY", "00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff")
	db := setupProviderTestDB(t)

	orgID := "org-alpha"
	plaintext := "my-secret-key-123"

	// Encrypt API key
	enc, err := EncryptKey(plaintext)
	if err != nil {
		t.Fatalf("EncryptKey error: %v", err)
	}

	// Save config to DB
	config := &OrgProviderConfig{
		OrgID:           orgID,
		ProviderName:    "anthropic",
		EncryptedAPIKey: enc,
		ActorModel:      "claude-3-5-sonnet",
		CriticModel:     "claude-opus-4-8",
	}
	if err := db.Create(config).Error; err != nil {
		t.Fatalf("save config error: %v", err)
	}

	// Retrieve config from DB
	fetched, err := GetProviderConfig(db, orgID)
	if err != nil {
		t.Fatalf("GetProviderConfig error: %v", err)
	}
	if fetched == nil {
		t.Fatalf("expected to fetch provider config, got nil")
	}
	if fetched.ActorModel != "claude-3-5-sonnet" {
		t.Errorf("expected actor model claude-3-5-sonnet, got %q", fetched.ActorModel)
	}
	if fetched.CriticModel != "claude-opus-4-8" {
		t.Errorf("expected critic model claude-opus-4-8, got %q", fetched.CriticModel)
	}

	// Verify key decryption
	dec, err := fetched.DecryptKey()
	if err != nil {
		t.Fatalf("DecryptKey error: %v", err)
	}
	if dec != plaintext {
		t.Errorf("expected decrypted key %q, got %q", plaintext, dec)
	}
}
