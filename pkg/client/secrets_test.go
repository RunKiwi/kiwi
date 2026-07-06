package client

import (
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestSecretLookup(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "secrets.json", `{"GITHUB_TOKEN":"from-file"}`)

	t.Setenv("ANTHROPIC_API_KEY", "from-env")
	t.Setenv("GITHUB_TOKEN", "env-should-lose")

	get := SecretLookup(path)

	if v := get("GITHUB_TOKEN"); v != "from-file" {
		t.Errorf("file precedence: got %q want from-file", v)
	}
	if v := get("ANTHROPIC_API_KEY"); v != "from-env" {
		t.Errorf("env fallback: got %q want from-env", v)
	}
	if v := get("MISSING"); v != "" {
		t.Errorf("missing key: got %q want empty", v)
	}
}

func TestSecretLookupNoFile(t *testing.T) {
	t.Setenv("ONLY_ENV", "yes")
	get := SecretLookup(filepath.Join(t.TempDir(), "does-not-exist.json"))
	if v := get("ONLY_ENV"); v != "yes" {
		t.Errorf("env-only: got %q want yes", v)
	}
}

func TestSecretLookupMalformed(t *testing.T) {
	dir := t.TempDir()
	path := writeFile(t, dir, "bad.json", `{not valid json`)
	t.Setenv("K", "v")
	get := SecretLookup(path)
	if v := get("K"); v != "v" {
		t.Errorf("malformed → env-only: got %q want v", v)
	}
}
