package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveToken(t *testing.T) {
	// Setup dummy config
	home, _ := os.UserHomeDir()
	cfgDir := filepath.Join(home, ".config", "kiwi")
	_ = os.MkdirAll(cfgDir, 0755)
	cfgPath := filepath.Join(cfgDir, "config.json")
	_ = os.WriteFile(cfgPath, []byte(`{"token":"config-token"}`), 0600)
	defer os.Remove(cfgPath)

	os.Setenv("KIWI_SERVER_TOKEN", "env-token")
	defer os.Unsetenv("KIWI_SERVER_TOKEN")

	// 1. Flag takes precedence
	if v := resolveToken("flag-token"); v != "flag-token" {
		t.Errorf("Expected flag-token, got %s", v)
	}

	// 2. Env takes precedence over config
	if v := resolveToken(""); v != "env-token" {
		t.Errorf("Expected env-token, got %s", v)
	}

	// 3. Config is fallback
	os.Unsetenv("KIWI_SERVER_TOKEN")
	if v := resolveToken(""); v != "config-token" {
		t.Errorf("Expected config-token, got %s", v)
	}
}
