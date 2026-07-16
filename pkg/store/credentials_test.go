package store

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/ibreakthecloud/kiwi/pkg/crypto"
)

func TestSaveAndGetCredential(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	if err := s.SaveCredential(ctx, "o1", "ANTHROPIC_API_KEY", CredentialLLM, "sk-ant-secret"); err != nil {
		t.Fatalf("SaveCredential: %v", err)
	}

	got, err := s.GetCredentialPlaintext(ctx, "o1", "ANTHROPIC_API_KEY")
	if err != nil {
		t.Fatalf("GetCredentialPlaintext: %v", err)
	}
	if got != "sk-ant-secret" {
		t.Errorf("got %q, want sk-ant-secret", got)
	}

	// Plaintext must never be stored in the DB column.
	var raw Credential
	s.DB().Where("org_id = ? AND name = ?", "o1", "ANTHROPIC_API_KEY").First(&raw)
	if raw.EncryptedValue == "" || raw.EncryptedValue == "sk-ant-secret" {
		t.Errorf("stored value must be ciphertext, got %q", raw.EncryptedValue)
	}
}

func TestSaveCredentialUpsert(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	_ = s.SaveCredential(ctx, "o1", "GITHUB_TOKEN", CredentialGit, "ghp_old")
	if err := s.SaveCredential(ctx, "o1", "GITHUB_TOKEN", CredentialGit, "ghp_new"); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	creds, err := s.ListCredentials(ctx, "o1")
	if err != nil {
		t.Fatalf("ListCredentials: %v", err)
	}
	if len(creds) != 1 {
		t.Fatalf("expected 1 credential after upsert, got %d", len(creds))
	}
	got, _ := s.GetCredentialPlaintext(ctx, "o1", "GITHUB_TOKEN")
	if got != "ghp_new" {
		t.Errorf("expected updated value ghp_new, got %q", got)
	}
}

func TestSealCredentialsForDaemon(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	_ = s.SaveCredential(ctx, "o1", "ANTHROPIC_API_KEY", CredentialLLM, "sk-ant-secret")
	_ = s.SaveCredential(ctx, "o1", "GITHUB_TOKEN", CredentialGit, "ghp_secret")

	// A daemon's X25519 identity.
	pub, priv, err := crypto.GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair: %v", err)
	}

	sealed, err := s.SealCredentialsForDaemon(ctx, "o1", pub)
	if err != nil {
		t.Fatalf("SealCredentialsForDaemon: %v", err)
	}
	if sealed == "" {
		t.Fatal("expected a sealed payload")
	}

	// Only the daemon's private key can open it, yielding the plaintext bundle.
	opened, err := crypto.OpenSealed(priv, sealed)
	if err != nil {
		t.Fatalf("OpenSealed: %v", err)
	}
	var bundle map[string]string
	if err := json.Unmarshal(opened, &bundle); err != nil {
		t.Fatalf("unmarshal bundle: %v", err)
	}
	if bundle["ANTHROPIC_API_KEY"] != "sk-ant-secret" || bundle["GITHUB_TOKEN"] != "ghp_secret" {
		t.Errorf("sealed bundle mismatch: %+v", bundle)
	}
}

func TestSealCredentialsForDaemonEmpty(t *testing.T) {
	s := newTestStore(t)
	pub, _, _ := crypto.GenerateKeyPair()
	sealed, err := s.SealCredentialsForDaemon(context.Background(), "org-with-no-creds", pub)
	if err != nil {
		t.Fatalf("SealCredentialsForDaemon: %v", err)
	}
	if sealed != "" {
		t.Errorf("expected empty payload for org with no credentials, got %q", sealed)
	}
}
