package store

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"testing"
	"time"
)

func mkOrg(t *testing.T, s *PostgresStore, id string) {
	t.Helper()
	if err := s.db.Create(&Organization{ID: id, Name: id}).Error; err != nil {
		t.Fatalf("create org %s: %v", id, err)
	}
}

func mkSignKey(t *testing.T) string {
	t.Helper()
	pub, _, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("gen key: %v", err)
	}
	return base64.StdEncoding.EncodeToString(pub)
}

func TestRegisterDaemon_HappyPath(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	mkOrg(t, s, "o1")

	token, err := s.CreateDaemonJoinToken(ctx, "o1", time.Hour)
	if err != nil {
		t.Fatalf("CreateDaemonJoinToken: %v", err)
	}
	if token == "" {
		t.Fatal("expected a non-empty join token")
	}

	sign := mkSignKey(t)
	d, err := s.RegisterDaemon(ctx, token, sign, "encpub")
	if err != nil {
		t.Fatalf("RegisterDaemon: %v", err)
	}
	if d.OrgID != "o1" {
		t.Errorf("org = %s, want o1 (must come from the token, not the request)", d.OrgID)
	}

	// The daemon resolves by its Ed25519 identity.
	got, err := s.GetDaemonBySignPubKey(ctx, sign)
	if err != nil {
		t.Fatalf("GetDaemonBySignPubKey: %v", err)
	}
	if got.ID != d.ID {
		t.Errorf("resolved daemon %s, want %s", got.ID, d.ID)
	}
}

func TestRegisterDaemon_TokenIsSingleUse(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	mkOrg(t, s, "o1")

	token, _ := s.CreateDaemonJoinToken(ctx, "o1", time.Hour)
	if _, err := s.RegisterDaemon(ctx, token, mkSignKey(t), "enc1"); err != nil {
		t.Fatalf("first register: %v", err)
	}

	// A second daemon presenting the same token must be rejected.
	_, err := s.RegisterDaemon(ctx, token, mkSignKey(t), "enc2")
	if !errors.Is(err, ErrJoinTokenUsed) {
		t.Errorf("second register err = %v, want ErrJoinTokenUsed", err)
	}
}

func TestRegisterDaemon_InvalidAndExpiredTokens(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	mkOrg(t, s, "o1")

	if _, err := s.RegisterDaemon(ctx, "nope", mkSignKey(t), "enc"); !errors.Is(err, ErrJoinTokenInvalid) {
		t.Errorf("unknown token err = %v, want ErrJoinTokenInvalid", err)
	}

	// Expired token.
	token, _ := s.CreateDaemonJoinToken(ctx, "o1", time.Hour)
	// Force expiry in the past.
	if err := s.db.Model(&DaemonJoinToken{}).
		Where("org_id = ?", "o1").
		Update("expires_at", time.Now().Add(-time.Minute)).Error; err != nil {
		t.Fatalf("expire token: %v", err)
	}
	if _, err := s.RegisterDaemon(ctx, token, mkSignKey(t), "enc"); !errors.Is(err, ErrJoinTokenExpired) {
		t.Errorf("expired token err = %v, want ErrJoinTokenExpired", err)
	}
}

func TestRegisterDaemon_ReRegisterRotatesEncKey(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	mkOrg(t, s, "o1")
	sign := mkSignKey(t)

	t1, _ := s.CreateDaemonJoinToken(ctx, "o1", time.Hour)
	d1, err := s.RegisterDaemon(ctx, t1, sign, "enc-old")
	if err != nil {
		t.Fatalf("first register: %v", err)
	}

	// Same identity, fresh token, new encryption key: rotation, not a new daemon.
	t2, _ := s.CreateDaemonJoinToken(ctx, "o1", time.Hour)
	d2, err := s.RegisterDaemon(ctx, t2, sign, "enc-new")
	if err != nil {
		t.Fatalf("re-register: %v", err)
	}
	if d1.ID != d2.ID {
		t.Errorf("re-register created a new daemon (%s != %s); want rotation", d1.ID, d2.ID)
	}
	if d2.EncPubKey != "enc-new" {
		t.Errorf("enc key = %s, want enc-new (rotation should update the seal target)", d2.EncPubKey)
	}
}

func TestGetDaemonBySignPubKey_Unknown(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.GetDaemonBySignPubKey(context.Background(), "unknown"); !errors.Is(err, ErrDaemonNotFound) {
		t.Errorf("err = %v, want ErrDaemonNotFound", err)
	}
}
