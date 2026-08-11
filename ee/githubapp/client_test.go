// SPDX-License-Identifier: LicenseRef-Kiwi-BSL-1.1
// Copyright (c) 2026 RunKiwi. Licensed under the Business Source License 1.1.
// See ee/LICENSE. This is Control Plane code and is NOT Apache-2.0.

package githubapp

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func testKeyPEM(t *testing.T) ([]byte, *rsa.PrivateKey) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	der := x509.MarshalPKCS1PrivateKey(key)
	return pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: der}), key
}

// stubGitHub serves the mint endpoint and counts how often it is called, which
// is how the cache tests distinguish "returned a token" from "minted a token".
func stubGitHub(t *testing.T, expiresIn time.Duration, calls *atomic.Int32) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if !strings.HasSuffix(r.URL.Path, "/access_tokens") {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); !strings.HasPrefix(got, "Bearer ") {
			t.Errorf("Authorization = %q, want a Bearer JWT", got)
		}
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(Token{
			Value:     fmt.Sprintf("ghs_stub_%d", calls.Load()),
			ExpiresAt: time.Now().Add(expiresIn),
		})
	}))
}

func TestInstallationTokenMints(t *testing.T) {
	var calls atomic.Int32
	srv := stubGitHub(t, time.Hour, &calls)
	defer srv.Close()

	pemKey, _ := testKeyPEM(t)
	c, err := New("123", pemKey, WithBaseURL(srv.URL))
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	tok, err := c.InstallationToken(context.Background(), 42)
	if err != nil {
		t.Fatalf("InstallationToken: %v", err)
	}
	if tok.Value != "ghs_stub_1" {
		t.Errorf("token = %q, want ghs_stub_1", tok.Value)
	}
}

// A second request inside the validity window must not mint again. Minting is
// rate-limited per App, so at fleet scale an uncached client is an outage.
func TestInstallationTokenCachesWithinWindow(t *testing.T) {
	var calls atomic.Int32
	srv := stubGitHub(t, time.Hour, &calls)
	defer srv.Close()

	pemKey, _ := testKeyPEM(t)
	c, _ := New("123", pemKey, WithBaseURL(srv.URL))

	for i := range 3 {
		if _, err := c.InstallationToken(context.Background(), 42); err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
	}
	if got := calls.Load(); got != 1 {
		t.Errorf("minted %d times, want 1", got)
	}
}

// A token inside the expiry margin is refused even though GitHub still
// considers it valid, because it may expire while git is still using it.
func TestInstallationTokenIgnoresCacheInsideExpiryMargin(t *testing.T) {
	var calls atomic.Int32
	srv := stubGitHub(t, expiryMargin-time.Minute, &calls)
	defer srv.Close()

	pemKey, _ := testKeyPEM(t)
	c, _ := New("123", pemKey, WithBaseURL(srv.URL))

	for i := range 2 {
		if _, err := c.InstallationToken(context.Background(), 42); err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
	}
	if got := calls.Load(); got != 2 {
		t.Errorf("minted %d times, want 2 (cache must not serve inside the margin)", got)
	}
}

func TestInstallationTokenPerInstallation(t *testing.T) {
	var calls atomic.Int32
	srv := stubGitHub(t, time.Hour, &calls)
	defer srv.Close()

	pemKey, _ := testKeyPEM(t)
	c, _ := New("123", pemKey, WithBaseURL(srv.URL))

	a, _ := c.InstallationToken(context.Background(), 1)
	b, _ := c.InstallationToken(context.Background(), 2)
	if a.Value == b.Value {
		t.Errorf("installations 1 and 2 shared token %q", a.Value)
	}
	if got := calls.Load(); got != 2 {
		t.Errorf("minted %d times, want 2", got)
	}
}

func TestInstallationTokenErrorMapping(t *testing.T) {
	cases := []struct {
		name   string
		status int
		want   error
	}{
		{"uninstalled", http.StatusNotFound, ErrInstallationGone},
		{"repo removed", http.StatusGone, ErrInstallationGone},
		{"bad app key", http.StatusUnauthorized, ErrAppAuth},
		{"app suspended", http.StatusForbidden, ErrAppAuth},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
			}))
			defer srv.Close()

			pemKey, _ := testKeyPEM(t)
			c, _ := New("123", pemKey, WithBaseURL(srv.URL))

			_, err := c.InstallationToken(context.Background(), 42)
			if !errors.Is(err, tc.want) {
				t.Errorf("err = %v, want %v", err, tc.want)
			}
		})
	}
}

// Revocation must take effect on the next task rather than whenever the cache
// happens to age out, so a gone installation purges any token already held.
func TestGoneInstallationPurgesCache(t *testing.T) {
	var gone atomic.Bool
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if gone.Load() {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(Token{Value: "ghs_live", ExpiresAt: time.Now().Add(time.Hour)})
	}))
	defer srv.Close()

	pemKey, _ := testKeyPEM(t)
	c, _ := New("123", pemKey, WithBaseURL(srv.URL))

	if _, err := c.InstallationToken(context.Background(), 42); err != nil {
		t.Fatalf("first mint: %v", err)
	}
	// Force the next call past the cache so it reaches the now-revoked stub.
	c.forget(42)
	gone.Store(true)

	if _, err := c.InstallationToken(context.Background(), 42); !errors.Is(err, ErrInstallationGone) {
		t.Fatalf("err = %v, want ErrInstallationGone", err)
	}
	if _, ok := c.cached(42); ok {
		t.Error("a revoked installation left a usable token in the cache")
	}
}

func TestMintRejectsEmptyToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"expires_at":"2099-01-01T00:00:00Z"}`))
	}))
	defer srv.Close()

	pemKey, _ := testKeyPEM(t)
	c, _ := New("123", pemKey, WithBaseURL(srv.URL))

	if _, err := c.InstallationToken(context.Background(), 42); err == nil {
		t.Fatal("want an error when GitHub returns no token, got nil")
	}
}

func TestAppJWTIsVerifiableAndBounded(t *testing.T) {
	pemKey, key := testKeyPEM(t)
	if _, err := ParsePrivateKey(pemKey); err != nil {
		t.Fatalf("ParsePrivateKey: %v", err)
	}

	now := time.Unix(1_700_000_000, 0)
	tok, err := appJWT("app-7", key, now)
	if err != nil {
		t.Fatalf("appJWT: %v", err)
	}

	parts := strings.Split(tok, ".")
	if len(parts) != 3 {
		t.Fatalf("jwt has %d segments, want 3", len(parts))
	}

	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		t.Fatalf("decode signature: %v", err)
	}
	if err := rsa.VerifyPKCS1v15(&key.PublicKey, crypto.SHA256, digest[:], sig); err != nil {
		t.Errorf("signature does not verify: %v", err)
	}

	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatalf("decode claims: %v", err)
	}
	var claims struct {
		Iat int64  `json:"iat"`
		Exp int64  `json:"exp"`
		Iss string `json:"iss"`
	}
	if err := json.Unmarshal(raw, &claims); err != nil {
		t.Fatalf("unmarshal claims: %v", err)
	}
	if claims.Iss != "app-7" {
		t.Errorf("iss = %q, want app-7", claims.Iss)
	}
	// Backdated, so a control plane clock running fast does not produce an
	// intermittent 401 that names nothing.
	if claims.Iat >= now.Unix() {
		t.Errorf("iat = %d, want < %d", claims.Iat, now.Unix())
	}
	// GitHub caps App JWT lifetime at 10 minutes and rejects anything longer.
	if lifetime := claims.Exp - now.Unix(); lifetime > int64((10 * time.Minute).Seconds()) {
		t.Errorf("exp is %ds out, want <= 600s", lifetime)
	}
}

func TestParsePrivateKeyRejectsGarbage(t *testing.T) {
	if _, err := ParsePrivateKey([]byte("not a pem")); err == nil {
		t.Fatal("want an error for non-PEM input, got nil")
	}
}

func TestNewRejectsEmptyAppID(t *testing.T) {
	pemKey, _ := testKeyPEM(t)
	if _, err := New("  ", pemKey); err == nil {
		t.Fatal("want an error for an empty app id, got nil")
	}
}
