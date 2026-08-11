// SPDX-License-Identifier: LicenseRef-Kiwi-BSL-1.1
// Copyright (c) 2026 RunKiwi. Licensed under the Business Source License 1.1.
// See ee/LICENSE. This is Control Plane code and is NOT Apache-2.0.

package orchestrator

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"github.com/ibreakthecloud/kiwi/ee/githubapp"
	"github.com/ibreakthecloud/kiwi/pkg/store"
)

func newInstallServer(t *testing.T, accountLogin string) (*Server, store.Store) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&store.Organization{}, &store.GitHubInstallation{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	st := store.NewPostgresStore(db)
	s := &Server{db: db, storage: st}

	gh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if accountLogin == "" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintf(w, `{"id":4242,"account":{"login":%q},"repository_selection":"selected"}`, accountLogin)
	}))
	t.Cleanup(gh.Close)

	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	pemKey := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	client, err := githubapp.New("1", pemKey, githubapp.WithBaseURL(gh.URL))
	if err != nil {
		t.Fatalf("githubapp.New: %v", err)
	}
	s.githubApp = client
	return s, st
}

func callbackRequest(t *testing.T, state, installationID string) *http.Request {
	t.Helper()
	return httptest.NewRequest(http.MethodGet,
		"/api/v1/github/callback?state="+state+"&installation_id="+installationID, nil)
}

func TestInstallStateRoundTrip(t *testing.T) {
	t.Setenv("KIWI_SESSION_SECRET", "test-secret")

	signed, err := signInstallState(installState{
		OrgID: "org_a", UserID: "u1", Nonce: "n", ExpiresAt: time.Now().Add(time.Minute).Unix(),
	})
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	got, err := verifyInstallState(signed)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if got.OrgID != "org_a" {
		t.Errorf("org = %q, want org_a", got.OrgID)
	}
}

// The state is the only thing binding an install to an org, so forging it is
// the attack that matters: it would bind an attacker's GitHub account to
// somebody else's tenant.
func TestInstallStateRejectsTampering(t *testing.T) {
	t.Setenv("KIWI_SESSION_SECRET", "test-secret")

	signed, _ := signInstallState(installState{
		OrgID: "org_victim", ExpiresAt: time.Now().Add(time.Minute).Unix(),
	})
	parts := strings.Split(signed, ".")

	forged, _ := signInstallState(installState{
		OrgID: "org_attacker", ExpiresAt: time.Now().Add(time.Minute).Unix(),
	})
	attackerPayload := strings.Split(forged, ".")[0]

	// Attacker's payload, victim's signature.
	if _, err := verifyInstallState(attackerPayload + "." + parts[1]); err == nil {
		t.Fatal("a swapped payload verified: the org binding is forgeable")
	}
}

func TestInstallStateRejectsExpired(t *testing.T) {
	t.Setenv("KIWI_SESSION_SECRET", "test-secret")
	signed, _ := signInstallState(installState{
		OrgID: "org_a", ExpiresAt: time.Now().Add(-time.Minute).Unix(),
	})
	if _, err := verifyInstallState(signed); err == nil {
		t.Fatal("an expired state verified")
	}
}

func TestInstallStateRejectsMalformed(t *testing.T) {
	t.Setenv("KIWI_SESSION_SECRET", "test-secret")
	for _, v := range []string{"", "nodot", "a.b.c", "!!.!!"} {
		if _, err := verifyInstallState(v); err == nil {
			t.Errorf("verifyInstallState(%q) succeeded, want an error", v)
		}
	}
}

func TestCallbackPersistsInstallation(t *testing.T) {
	t.Setenv("KIWI_SESSION_SECRET", "test-secret")
	s, st := newInstallServer(t, "RunKiwi")

	state, _ := signInstallState(installState{
		OrgID: "org_a", ExpiresAt: time.Now().Add(time.Minute).Unix(),
	})

	rec := httptest.NewRecorder()
	s.handleGithubCallback(rec, callbackRequest(t, state, "4242"))

	if rec.Code != http.StatusFound {
		t.Fatalf("status = %d, want 302 (body: %s)", rec.Code, rec.Body.String())
	}
	inst, err := st.FindGitHubInstallation(t.Context(), "org_a", "runkiwi")
	if err != nil {
		t.Fatalf("installation not persisted: %v", err)
	}
	if inst.InstallationID != 4242 {
		t.Errorf("installation id = %d, want 4242", inst.InstallationID)
	}
}

// The account comes from GitHub, never from the redirect. A caller who guesses
// an installation id must not be able to claim an account by naming it.
func TestCallbackTakesAccountFromGitHubNotTheRequest(t *testing.T) {
	t.Setenv("KIWI_SESSION_SECRET", "test-secret")
	s, st := newInstallServer(t, "real-account")

	state, _ := signInstallState(installState{
		OrgID: "org_a", ExpiresAt: time.Now().Add(time.Minute).Unix(),
	})
	req := httptest.NewRequest(http.MethodGet,
		"/api/v1/github/callback?state="+state+"&installation_id=4242&account_login=spoofed", nil)

	rec := httptest.NewRecorder()
	s.handleGithubCallback(rec, req)

	if _, err := st.FindGitHubInstallation(t.Context(), "org_a", "spoofed"); err == nil {
		t.Fatal("the account was taken from the request")
	}
	if _, err := st.FindGitHubInstallation(t.Context(), "org_a", "real-account"); err != nil {
		t.Fatalf("the account GitHub reported was not stored: %v", err)
	}
}

func TestCallbackRejectsBadState(t *testing.T) {
	t.Setenv("KIWI_SESSION_SECRET", "test-secret")
	s, _ := newInstallServer(t, "RunKiwi")

	rec := httptest.NewRecorder()
	s.handleGithubCallback(rec, callbackRequest(t, "forged.state", "4242"))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestCallbackRejectsMissingInstallationID(t *testing.T) {
	t.Setenv("KIWI_SESSION_SECRET", "test-secret")
	s, _ := newInstallServer(t, "RunKiwi")

	state, _ := signInstallState(installState{
		OrgID: "org_a", ExpiresAt: time.Now().Add(time.Minute).Unix(),
	})
	rec := httptest.NewRecorder()
	s.handleGithubCallback(rec, callbackRequest(t, state, "not-a-number"))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestCallbackReportsVanishedInstallation(t *testing.T) {
	t.Setenv("KIWI_SESSION_SECRET", "test-secret")
	s, _ := newInstallServer(t, "") // stub 404s

	state, _ := signInstallState(installState{
		OrgID: "org_a", ExpiresAt: time.Now().Add(time.Minute).Unix(),
	})
	rec := httptest.NewRecorder()
	s.handleGithubCallback(rec, callbackRequest(t, state, "4242"))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestInstallEndpointNeedsConfiguredApp(t *testing.T) {
	db, _ := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	_ = db.AutoMigrate(&store.GitHubInstallation{})
	s := &Server{db: db, storage: store.NewPostgresStore(db)}

	rec := httptest.NewRecorder()
	s.handleGithubCallback(rec, httptest.NewRequest(http.MethodGet, "/api/v1/github/callback", nil))

	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("status = %d, want 501 when no app is configured", rec.Code)
	}
}

// Revocation has to land without anyone running a task first, or the row keeps
// routing work away from the GIT_TOKEN fallback into a delayed failure.
func TestInstallationDeletedWebhookRemovesRow(t *testing.T) {
	t.Setenv("GITHUB_WEBHOOK_SECRET", "hook-secret")
	s, st := newInstallServer(t, "RunKiwi")

	if err := st.UpsertGitHubInstallation(t.Context(), &store.GitHubInstallation{
		InstallationID: 4242, OrgID: "org_a", AccountLogin: "runkiwi",
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	body, _ := json.Marshal(map[string]any{
		"action":       "deleted",
		"installation": map[string]any{"id": 4242},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/github", strings.NewReader(string(body)))
	req.Header.Set("X-GitHub-Event", "installation")
	req.Header.Set("X-Hub-Signature-256", generateSignature([]byte("hook-secret"), body))

	rec := httptest.NewRecorder()
	s.handleGithubWebhook(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if _, err := st.FindGitHubInstallation(t.Context(), "org_a", "runkiwi"); err == nil {
		t.Error("installation survived the deleted webhook")
	}
}
