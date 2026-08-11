// SPDX-License-Identifier: LicenseRef-Kiwi-BSL-1.1
// Copyright (c) 2026 RunKiwi. Licensed under the Business Source License 1.1.
// See ee/LICENSE. This is Control Plane code and is NOT Apache-2.0.

package orchestrator

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"github.com/ibreakthecloud/kiwi/ee/githubapp"
	"github.com/ibreakthecloud/kiwi/pkg/crypto"
	"github.com/ibreakthecloud/kiwi/pkg/daemon"
	"github.com/ibreakthecloud/kiwi/pkg/store"
)

type gitTokenFixture struct {
	ts       *httptest.Server
	st       store.Store
	signPub  ed25519.PublicKey
	signPriv ed25519.PrivateKey
	daemonID string
}

// stubMintServer stands in for api.github.com's token-mint endpoint.
func stubMintServer(t *testing.T, status int) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if status != http.StatusCreated {
			w.WriteHeader(status)
			return
		}
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"token":      "ghs_minted",
			"expires_at": time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
		})
	}))
	t.Cleanup(srv.Close)
	return srv
}

func newGitTokenFixture(t *testing.T, withApp bool, mintStatus int) *gitTokenFixture {
	t.Helper()

	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(
		&store.Organization{}, &store.QueuedTask{}, &store.Daemon{}, &store.GitHubInstallation{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	st := store.NewPostgresStore(db)
	s := &Server{db: db, storage: st}

	if withApp {
		key, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			t.Fatalf("gen rsa: %v", err)
		}
		pemKey := pem.EncodeToMemory(&pem.Block{
			Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key),
		})
		mint := stubMintServer(t, mintStatus)
		client, err := githubapp.New("1", pemKey, githubapp.WithBaseURL(mint.URL))
		if err != nil {
			t.Fatalf("githubapp.New: %v", err)
		}
		s.githubApp = client
	}

	signPub, signPriv, err := crypto.GenerateSigningKeyPair()
	if err != nil {
		t.Fatalf("gen sign key: %v", err)
	}
	signPubB64 := base64.StdEncoding.EncodeToString(signPub)

	if err := db.Create(&store.Daemon{
		ID: "daemon_1", OrgID: "org_a", SignPubKey: signPubB64, EncPubKey: "enc",
	}).Error; err != nil {
		t.Fatalf("seed daemon: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/daemon/git-token", s.handleDaemonGitToken)
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)

	return &gitTokenFixture{ts: ts, st: st, signPub: signPub, signPriv: signPriv, daemonID: "daemon_1"}
}

func (f *gitTokenFixture) seedTask(t *testing.T, taskID, orgID, leaseID, repoURL, leasedBy string) {
	t.Helper()
	expires := time.Now().UTC().Add(time.Minute)
	err := f.st.DB().Create(&store.QueuedTask{
		ID: taskID, OrgID: orgID, Status: "LEASED",
		Spec:           map[string]any{"repo_url": repoURL},
		LeaseID:        &leaseID,
		LeasedBy:       &leasedBy,
		LeaseExpiresAt: &expires,
	}).Error
	if err != nil {
		t.Fatalf("seed task: %v", err)
	}
}

func (f *gitTokenFixture) post(t *testing.T, req daemon.GitTokenReq, sign bool) *http.Response {
	t.Helper()
	body, _ := json.Marshal(req)
	httpReq, _ := http.NewRequest(http.MethodPost, f.ts.URL+"/api/v1/daemon/git-token", bytes.NewReader(body))
	if sign {
		httpReq.Header.Set("X-Kiwi-Signature", base64.StdEncoding.EncodeToString(ed25519.Sign(f.signPriv, body)))
	}
	resp, err := http.DefaultClient.Do(httpReq)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

func (f *gitTokenFixture) req() daemon.GitTokenReq {
	return daemon.GitTokenReq{
		TaskID: "task_1", LeaseID: "lease_good",
		SignPubKey: base64.StdEncoding.EncodeToString(f.signPub),
	}
}

func TestGitTokenMintsForInstalledRepo(t *testing.T) {
	f := newGitTokenFixture(t, true, http.StatusCreated)
	f.seedTask(t, "task_1", "org_a", "lease_good", "https://github.com/RunKiwi/kiwi.git", "daemon_1")
	if err := f.st.UpsertGitHubInstallation(t.Context(), &store.GitHubInstallation{
		InstallationID: 9, OrgID: "org_a", AccountLogin: "RunKiwi",
	}); err != nil {
		t.Fatalf("seed installation: %v", err)
	}

	resp := f.post(t, f.req(), true)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var out daemon.GitTokenResp
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out.Token != "ghs_minted" {
		t.Errorf("token = %q, want ghs_minted", out.Token)
	}
}

// The tenancy boundary, and the single most important test in this file. A task
// in org_a must not resolve an installation that belongs to org_b, even when
// the repository owner matches exactly.
func TestGitTokenWillNotCrossOrgs(t *testing.T) {
	f := newGitTokenFixture(t, true, http.StatusCreated)
	f.seedTask(t, "task_1", "org_a", "lease_good", "https://github.com/victim-co/secrets.git", "daemon_1")
	if err := f.st.UpsertGitHubInstallation(t.Context(), &store.GitHubInstallation{
		InstallationID: 9, OrgID: "org_b", AccountLogin: "victim-co",
	}); err != nil {
		t.Fatalf("seed installation: %v", err)
	}

	if resp := f.post(t, f.req(), true); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404: one org minted against another org's installation", resp.StatusCode)
	}
}

func TestGitTokenRejectsUnsignedRequest(t *testing.T) {
	f := newGitTokenFixture(t, true, http.StatusCreated)
	f.seedTask(t, "task_1", "org_a", "lease_good", "https://github.com/RunKiwi/kiwi", "daemon_1")

	if resp := f.post(t, f.req(), false); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}

func TestGitTokenRejectsWrongLease(t *testing.T) {
	f := newGitTokenFixture(t, true, http.StatusCreated)
	f.seedTask(t, "task_1", "org_a", "lease_good", "https://github.com/RunKiwi/kiwi", "daemon_1")

	req := f.req()
	req.LeaseID = "lease_stolen"
	if resp := f.post(t, req, true); resp.StatusCode != http.StatusConflict {
		t.Fatalf("status = %d, want 409", resp.StatusCode)
	}
}

// The fencing token matches but the lease belongs to a different daemon.
func TestGitTokenRejectsLeaseHeldByAnotherDaemon(t *testing.T) {
	f := newGitTokenFixture(t, true, http.StatusCreated)
	f.seedTask(t, "task_1", "org_a", "lease_good", "https://github.com/RunKiwi/kiwi", "daemon_other")

	if resp := f.post(t, f.req(), true); resp.StatusCode != http.StatusConflict {
		t.Fatalf("status = %d, want 409", resp.StatusCode)
	}
}

// 404 is the fallback signal: the daemon uses its sealed GIT_TOKEN instead.
// These three cases are what keep PAT orgs and non-GitHub remotes working.
func TestGitTokenFallsBackTo404(t *testing.T) {
	t.Run("no installation for owner", func(t *testing.T) {
		f := newGitTokenFixture(t, true, http.StatusCreated)
		f.seedTask(t, "task_1", "org_a", "lease_good", "https://github.com/RunKiwi/kiwi", "daemon_1")
		if resp := f.post(t, f.req(), true); resp.StatusCode != http.StatusNotFound {
			t.Fatalf("status = %d, want 404", resp.StatusCode)
		}
	})

	t.Run("non-github remote", func(t *testing.T) {
		f := newGitTokenFixture(t, true, http.StatusCreated)
		f.seedTask(t, "task_1", "org_a", "lease_good", "https://gitlab.com/acme/widgets.git", "daemon_1")
		if resp := f.post(t, f.req(), true); resp.StatusCode != http.StatusNotFound {
			t.Fatalf("status = %d, want 404", resp.StatusCode)
		}
	})

	t.Run("no app configured", func(t *testing.T) {
		f := newGitTokenFixture(t, false, 0)
		f.seedTask(t, "task_1", "org_a", "lease_good", "https://github.com/RunKiwi/kiwi", "daemon_1")
		if resp := f.post(t, f.req(), true); resp.StatusCode != http.StatusNotFound {
			t.Fatalf("status = %d, want 404", resp.StatusCode)
		}
	})
}

// A revoked installation must not keep routing tasks into an error: the row is
// dropped so the next submit can fail fast asking the customer to reconnect.
func TestGitTokenClearsRevokedInstallation(t *testing.T) {
	f := newGitTokenFixture(t, true, http.StatusNotFound)
	f.seedTask(t, "task_1", "org_a", "lease_good", "https://github.com/RunKiwi/kiwi", "daemon_1")
	if err := f.st.UpsertGitHubInstallation(t.Context(), &store.GitHubInstallation{
		InstallationID: 9, OrgID: "org_a", AccountLogin: "RunKiwi",
	}); err != nil {
		t.Fatalf("seed installation: %v", err)
	}

	if resp := f.post(t, f.req(), true); resp.StatusCode != http.StatusGone {
		t.Fatalf("status = %d, want 410", resp.StatusCode)
	}
	if _, err := f.st.FindGitHubInstallation(t.Context(), "org_a", "RunKiwi"); err == nil {
		t.Error("revoked installation was left in place")
	}
}
