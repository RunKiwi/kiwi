package orchestrator

import (
	"os"
	"testing"
)

func TestLoadAndValidateConfig(t *testing.T) {
	// Clean up environment after tests
	defer func() {
		os.Unsetenv("KIWI_ENV")
		os.Unsetenv("KIWI_ENCRYPTION_KEY")
		os.Unsetenv("KIWI_SERVER_TOKEN")
		os.Unsetenv("KIWI_CORS_ALLOWED_ORIGINS")
	}()

	t.Run("dev mode allows missing secrets", func(t *testing.T) {
		os.Setenv("KIWI_ENV", "development")
		os.Unsetenv("KIWI_ENCRYPTION_KEY")

		cfg, err := LoadAndValidateConfig(":8080", "dsn", "all", "nats")
		if err != nil {
			t.Fatalf("expected success, got error: %v", err)
		}
		if cfg.Env != "development" {
			t.Errorf("expected env development, got %s", cfg.Env)
		}
	})

	t.Run("prod mode fails if encryption key missing", func(t *testing.T) {
		os.Setenv("KIWI_ENV", "production")
		os.Unsetenv("KIWI_ENCRYPTION_KEY")
		os.Setenv("KIWI_SERVER_TOKEN", "token")
		os.Setenv("KIWI_CORS_ALLOWED_ORIGINS", "https://app.kiwi.com")

		_, err := LoadAndValidateConfig(":8080", "dsn", "all", "nats")
		if err == nil {
			t.Fatal("expected error for missing encryption key")
		}
	})

	t.Run("prod mode fails if server token missing", func(t *testing.T) {
		os.Setenv("KIWI_ENV", "production")
		os.Setenv("KIWI_ENCRYPTION_KEY", "00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff")
		os.Unsetenv("KIWI_SERVER_TOKEN")
		os.Setenv("KIWI_CORS_ALLOWED_ORIGINS", "https://app.kiwi.com")

		_, err := LoadAndValidateConfig(":8080", "dsn", "all", "nats")
		if err == nil {
			t.Fatal("expected error for missing server token")
		}
	})

	t.Run("prod mode fails on wildcard CORS", func(t *testing.T) {
		os.Setenv("KIWI_ENV", "production")
		os.Setenv("KIWI_ENCRYPTION_KEY", "00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff")
		os.Setenv("KIWI_SERVER_TOKEN", "token")
		os.Setenv("KIWI_CORS_ALLOWED_ORIGINS", "*")

		_, err := LoadAndValidateConfig(":8080", "dsn", "all", "nats")
		if err == nil {
			t.Fatal("expected error for wildcard CORS")
		}
	})

	t.Run("prod mode succeeds with valid config", func(t *testing.T) {
		os.Setenv("KIWI_ENV", "production")
		os.Setenv("KIWI_ENCRYPTION_KEY", "00112233445566778899aabbccddeeff00112233445566778899aabbccddeeff")
		os.Setenv("KIWI_SERVER_TOKEN", "token")
		os.Setenv("KIWI_CORS_ALLOWED_ORIGINS", "https://app.kiwi.com")

		_, err := LoadAndValidateConfig(":8080", "dsn", "all", "nats")
		if err != nil {
			t.Fatalf("expected success, got error: %v", err)
		}
	})
}
