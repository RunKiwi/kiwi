package daemon

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"testing"
	"time"

	"github.com/ibreakthecloud/kiwi/pkg/crypto"
)

func TestBuildHeartbeatIncludesCacheAndMemStats(t *testing.T) {
	cacheDir := t.TempDir()
	cfg := Config{
		APIURL:   "http://localhost:8080",
		KeyPath:  "", // ephemeral key
		CacheDir: cacheDir,
	}

	d, err := New(cfg)
	if err != nil {
		t.Fatalf("New daemon failed: %v", err)
	}

	if err := d.Start(); err != nil {
		t.Fatalf("Start daemon failed: %v", err)
	}

	before := time.Now().Unix()
	req := d.buildHeartbeatReq(context.Background())
	after := time.Now().Unix()

	if req.PubKey == "" {
		t.Fatal("PubKey should not be empty")
	}
	if req.SignPubKey == "" {
		t.Fatal("SignPubKey should not be empty")
	}
	if req.Timestamp < before || req.Timestamp > after {
		t.Fatalf("Timestamp = %d, expected between %d and %d", req.Timestamp, before, after)
	}

	if req.CacheStats == nil {
		t.Fatal("CacheStats should be populated when daemon has a gitCache")
	}
	if req.CacheStats.TotalRepos != 0 {
		t.Errorf("CacheStats.TotalRepos = %d, want 0", req.CacheStats.TotalRepos)
	}
	if req.CacheStats.TotalActiveWorktrees != 0 {
		t.Errorf("CacheStats.TotalActiveWorktrees = %d, want 0", req.CacheStats.TotalActiveWorktrees)
	}
	if req.CacheStats.HitCount != 0 {
		t.Errorf("CacheStats.HitCount = %d, want 0", req.CacheStats.HitCount)
	}
	if req.CacheStats.MissCount != 0 {
		t.Errorf("CacheStats.MissCount = %d, want 0", req.CacheStats.MissCount)
	}
}

func TestBuildHeartbeatNilCache(t *testing.T) {
	pub, priv, err := crypto.GenerateKeyPair()
	if err != nil {
		t.Fatalf("GenerateKeyPair: %v", err)
	}
	signPub, signPriv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}

	d := &Daemon{
		pubKey:      pub,
		priKey:      priv,
		signPubKey:  signPub,
		signPrivKey: signPriv,
		gitCache:    nil,
	}

	req := d.buildHeartbeatReq(context.Background())
	if req.CacheStats != nil {
		t.Fatalf("CacheStats should be nil when gitCache is nil, got %+v", req.CacheStats)
	}
	if req.PubKey == "" {
		t.Fatal("PubKey should not be empty")
	}
	if req.SignPubKey == "" {
		t.Fatal("SignPubKey should not be empty")
	}
	if req.Timestamp == 0 {
		t.Fatal("Timestamp should not be 0")
	}
}
