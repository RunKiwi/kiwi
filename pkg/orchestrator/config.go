package orchestrator

import (
	"fmt"
	"os"
)

type Config struct {
	Addr               string
	DSN                string
	Role               string
	NatsURL            string
	Env                string
	EncryptionKey      string
	ServerToken        string
	CORSAllowedOrigins string
}

func getEnvOrDefault(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}

// LoadAndValidateConfig reads configuration from environment variables, falling back
// to the provided flag values. In production (KIWI_ENV=production), it enforces
// strict security requirements.
func LoadAndValidateConfig(flagAddr, flagDSN, flagRole, flagNats string) (*Config, error) {
	cfg := &Config{
		Addr:               getEnvOrDefault("KIWI_ADDR", flagAddr),
		DSN:                getEnvOrDefault("KIWI_DSN", flagDSN),
		Role:               getEnvOrDefault("KIWI_ROLE", flagRole),
		NatsURL:            getEnvOrDefault("KIWI_NATS_URL", flagNats),
		Env:                os.Getenv("KIWI_ENV"),
		EncryptionKey:      os.Getenv("KIWI_ENCRYPTION_KEY"),
		ServerToken:        os.Getenv("KIWI_SERVER_TOKEN"),
		CORSAllowedOrigins: os.Getenv("KIWI_CORS_ALLOWED_ORIGINS"),
	}

	if cfg.Env == "production" {
		if len(cfg.EncryptionKey) != 64 {
			return nil, fmt.Errorf("production security error: KIWI_ENCRYPTION_KEY must be a 64-character hex string")
		}
		if cfg.ServerToken == "" {
			return nil, fmt.Errorf("production security error: KIWI_SERVER_TOKEN must be set")
		}
		if cfg.CORSAllowedOrigins == "" || cfg.CORSAllowedOrigins == "*" {
			return nil, fmt.Errorf("production security error: KIWI_CORS_ALLOWED_ORIGINS must be set to specific origins (not wildcard)")
		}
	}

	// Ensure the os env is updated if it wasn't set, so that down-stream reads (like in crypto or corsMiddleware) match.
	// Actually we just read from env in corsMiddleware. Let's make sure KIWI_CORS_ALLOWED_ORIGINS is set in env if we wanted to fallback,
	// but we don't have a flag for CORS.

	return cfg, nil
}

// Log logs the non-secret configuration values.
func (c *Config) Log() {
	fmt.Println("=== Configuration ===")
	fmt.Printf("  Environment: %s\n", c.Env)
	fmt.Printf("  Addr:        %s\n", c.Addr)
	fmt.Printf("  Role:        %s\n", c.Role)
	fmt.Printf("  NatsURL:     %s\n", c.NatsURL)
	fmt.Println("=====================")
}
