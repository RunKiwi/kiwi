// SPDX-License-Identifier: LicenseRef-Kiwi-BSL-1.1
// Copyright (c) 2026 RunKiwi. Licensed under the Business Source License 1.1.
// See ee/LICENSE. This is Control Plane code and is NOT Apache-2.0.

package orchestrator

import (
	"bytes"
	"context"
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

	"github.com/ibreakthecloud/kiwi/ee/githubapp"
	"github.com/ibreakthecloud/kiwi/pkg/store"
)

// revertPRPayload builds a MERGED revert-PR payload — checkForRevert only
// treats a revert as a regression signal once GitHub's merge action itself
// has authorized it (see checkForRevert's doc comment).
func revertPRPayload(revertedSHA string) []byte {
	payload := map[string]any{
		"action":       "closed",
		"installation": map[string]any{"id": testGitHubInstallationID},
		"pull_request": map[string]any{
			"number":           43,
			"title":            `Revert "add a health endpoint"`,
			"body":             "This reverts commit " + revertedSHA + ".",
			"html_url":         "https://github.com/acme/widgets/pull/43",
			"merged":           true,
			"merged_at":        "2026-08-16T00:00:00Z",
			"merge_commit_sha": "ffffffffffffffffffffffffffffffffffffffff",
		},
		"repository": map[string]any{"name": "widgets", "owner": map[string]any{"login": "acme"}},
	}
	b, _ := json.Marshal(payload)
	return b
}

// unmergedRevertPRPayload builds an OPENED (not merged) revert-PR payload —
// the shape an unauthenticated third party could produce on any repo the
// GitHub App is installed on. Used to prove that opening such a PR, without
// it being merged, must never finalize a monitor.
func unmergedRevertPRPayload(revertedSHA string) []byte {
	payload := map[string]any{
		"action":       "opened",
		"installation": map[string]any{"id": testGitHubInstallationID},
		"pull_request": map[string]any{
			"number":   43,
			"title":    `Revert "add a health endpoint"`,
			"body":     "This reverts commit " + revertedSHA + ".",
			"html_url": "https://github.com/acme/widgets/pull/43",
			"merged":   false,
		},
		"repository": map[string]any{"name": "widgets", "owner": map[string]any{"login": "acme"}},
	}
	b, _ := json.Marshal(payload)
	return b
}

func TestRevertPRFinalizesMonitorAsRegression(t *testing.T) {
	withCommentSecret(t)
	srv, s := setupWebhookTest(t)
	mon := seedMonitorWithRecord(t, s, "org1", "job1", false)
	// seedMonitorWithRecord (postmerge_finalize_test.go) creates the monitor
	// with a fixed MergeCommitSHA — the revert payload below references it
	// via mon.MergeCommitSHA rather than a hardcoded literal.

	body := revertPRPayload(mon.MergeCommitSHA)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/github", bytes.NewReader(body))
	req.Header.Set("X-GitHub-Event", "pull_request")
	req.Header.Set("X-Hub-Signature-256", generateSignature([]byte(commentWebhookSecret), body))
	rec := httptest.NewRecorder()
	srv.handleGithubWebhook(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var got store.PostMergeMonitor
	if err := s.DB().First(&got, "id = ?", mon.ID).Error; err != nil {
		t.Fatal(err)
	}
	if got.Status != store.MonitorStatusRegression {
		t.Errorf("status = %q, want REGRESSION", got.Status)
	}
}

// TestUnmergedRevertPRDoesNotFinalizeMonitor is the negative case that used
// to be exploitable: before the authorization gate, ANYONE who could open a
// PR on an installed repo — on a public repo, anyone at all — could put a
// forged revert body in it and force a REGRESSION verdict with no write
// access whatsoever. Opening such a PR without merging it must 200 and leave
// the monitor untouched.
func TestUnmergedRevertPRDoesNotFinalizeMonitor(t *testing.T) {
	withCommentSecret(t)
	srv, s := setupWebhookTest(t)
	mon := seedMonitorWithRecord(t, s, "org1", "job1", false)

	body := unmergedRevertPRPayload(mon.MergeCommitSHA)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/github", bytes.NewReader(body))
	req.Header.Set("X-GitHub-Event", "pull_request")
	req.Header.Set("X-Hub-Signature-256", generateSignature([]byte(commentWebhookSecret), body))
	rec := httptest.NewRecorder()
	srv.handleGithubWebhook(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var got store.PostMergeMonitor
	if err := s.DB().First(&got, "id = ?", mon.ID).Error; err != nil {
		t.Fatal(err)
	}
	if got.Status != store.MonitorStatusMonitoring {
		t.Errorf("status = %q, want unchanged MONITORING — an unmerged PR must not authorize a REGRESSION verdict", got.Status)
	}
}

// TestNonRevertPROpenedDoesNotTouchMonitors now proves the action != "closed"
// gate (an opened PR is ignored outright before the revert pattern is even
// checked), rather than the revert-body match itself — that case is covered
// separately by TestUnmergedRevertPRDoesNotFinalizeMonitor above.
func TestNonRevertPROpenedDoesNotTouchMonitors(t *testing.T) {
	withCommentSecret(t)
	srv, s := setupWebhookTest(t)
	mon := seedMonitorWithRecord(t, s, "org1", "job1", false)

	payload := map[string]any{
		"action": "opened",
		"pull_request": map[string]any{
			"number": 44, "title": "Add caching", "body": "Adds a cache layer.",
			"html_url": "https://github.com/acme/widgets/pull/44",
		},
		"repository": map[string]any{"name": "widgets", "owner": map[string]any{"login": "acme"}},
	}
	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/github", bytes.NewReader(body))
	req.Header.Set("X-GitHub-Event", "pull_request")
	req.Header.Set("X-Hub-Signature-256", generateSignature([]byte(commentWebhookSecret), body))
	rec := httptest.NewRecorder()
	srv.handleGithubWebhook(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var got store.PostMergeMonitor
	if err := s.DB().First(&got, "id = ?", mon.ID).Error; err != nil {
		t.Fatal(err)
	}
	if got.Status != store.MonitorStatusMonitoring {
		t.Errorf("status = %q, want unchanged MONITORING", got.Status)
	}
}

// githubAppFixture builds a *Server whose githubApp mints tokens against a
// stub server, exactly like newGitTokenFixture (github_app_api_test.go) but
// without the daemon git-token HTTP plumbing this test doesn't need.
func githubAppFixture(t *testing.T) *Server {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("gen rsa: %v", err)
	}
	pemKey := pem.EncodeToMemory(&pem.Block{
		Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key),
	})
	mint := stubMintServer(t, http.StatusCreated)
	client, err := githubapp.New("1", pemKey, githubapp.WithBaseURL(mint.URL))
	if err != nil {
		t.Fatalf("githubapp.New: %v", err)
	}
	return &Server{githubApp: client}
}

// revertButtonPayload builds a payload whose body is GitHub's documented
// Revert-button template — a bare "Reverts #N", with no owner/repo prefix,
// since the button only ever reverts a PR in the same repository.
func revertButtonPayload(number int) githubWebhookPayload {
	var p githubWebhookPayload
	p.Installation.ID = testGitHubInstallationID
	p.PullRequest.Body = fmt.Sprintf("Reverts #%d", number)
	p.Repository.Owner.Login = "acme"
	p.Repository.Name = "widgets"
	return p
}

func TestResolveRevertedSHAFromBareRevertButtonBody(t *testing.T) {
	srv := githubAppFixture(t)
	wantSHA := strings.Repeat("b", 40)
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/acme/widgets/pulls/41" {
			t.Errorf("path = %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"merged":true,"merge_commit_sha":"` + wantSHA + `"}`))
	}))
	defer api.Close()

	payload := revertButtonPayload(41)
	inst := &store.GitHubInstallation{InstallationID: testGitHubInstallationID, OrgID: "org1"}

	sha, ok := srv.resolveRevertedSHA(context.Background(), payload, inst, api.URL)
	if !ok {
		t.Fatal("resolveRevertedSHA returned ok=false, want true")
	}
	if sha != wantSHA {
		t.Errorf("sha = %q, want %q", sha, wantSHA)
	}
}

// TestResolveRevertedSHAAcceptsQualifiedFormMatchingOwnRepo covers the
// owner/repo-qualified body shape too, in case a human edits the generated
// body or a future GitHub version reintroduces the prefix — as long as it
// names the reverting PR's own repository.
func TestResolveRevertedSHAAcceptsQualifiedFormMatchingOwnRepo(t *testing.T) {
	srv := githubAppFixture(t)
	wantSHA := strings.Repeat("b", 40)
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"merged":true,"merge_commit_sha":"` + wantSHA + `"}`))
	}))
	defer api.Close()

	payload := revertButtonPayload(41)
	payload.PullRequest.Body = "Reverts acme/widgets#41"
	inst := &store.GitHubInstallation{InstallationID: testGitHubInstallationID, OrgID: "org1"}

	sha, ok := srv.resolveRevertedSHA(context.Background(), payload, inst, api.URL)
	if !ok {
		t.Fatal("resolveRevertedSHA returned ok=false, want true")
	}
	if sha != wantSHA {
		t.Errorf("sha = %q, want %q", sha, wantSHA)
	}
}

// TestResolveRevertedSHARejectsQualifiedFormForADifferentRepo proves a body
// naming some other repository is ignored outright — not fetched — because
// the button never produces that shape and resolving it would be acting on
// unverified input rather than GitHub's own authorization signal.
func TestResolveRevertedSHARejectsQualifiedFormForADifferentRepo(t *testing.T) {
	srv := githubAppFixture(t)
	called := false
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		_, _ = w.Write([]byte(`{"merged":true,"merge_commit_sha":"` + strings.Repeat("d", 40) + `"}`))
	}))
	defer api.Close()

	payload := revertButtonPayload(41)
	payload.PullRequest.Body = "Reverts someoneelse/otherrepo#41"
	inst := &store.GitHubInstallation{InstallationID: testGitHubInstallationID, OrgID: "org1"}

	if _, ok := srv.resolveRevertedSHA(context.Background(), payload, inst, api.URL); ok {
		t.Error("resolveRevertedSHA returned ok=true for a body naming a different repo, want false")
	}
	if called {
		t.Error("resolveRevertedSHA called the GitHub API for a body naming a different repo, want no call")
	}
}

func TestResolveRevertedSHAIgnoresUnmergedReferencedPR(t *testing.T) {
	srv := githubAppFixture(t)
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"merged":false,"merge_commit_sha":null}`))
	}))
	defer api.Close()

	payload := revertButtonPayload(41)
	inst := &store.GitHubInstallation{InstallationID: testGitHubInstallationID, OrgID: "org1"}

	if _, ok := srv.resolveRevertedSHA(context.Background(), payload, inst, api.URL); ok {
		t.Error("resolveRevertedSHA returned ok=true for an unmerged referenced PR, want false")
	}
}

// TestResolveRevertedSHAWithoutGithubAppSkipsAPILookup proves the API-backed
// path degrades to a no-op rather than a panic or a network call when no App
// is configured — the same nil-githubApp state setupWebhookTest exercises for
// every other GitHub-calling path in this package.
func TestResolveRevertedSHAWithoutGithubAppSkipsAPILookup(t *testing.T) {
	srv := &Server{}
	payload := revertButtonPayload(41)
	inst := &store.GitHubInstallation{InstallationID: testGitHubInstallationID, OrgID: "org1"}

	if _, ok := srv.resolveRevertedSHA(context.Background(), payload, inst, "http://unused.invalid"); ok {
		t.Error("resolveRevertedSHA returned ok=true with no githubApp configured, want false")
	}
}

// TestResolveRevertedSHAPrefersCommitMessagePattern proves the cheaper,
// API-free path is tried first: a manually authored `git revert` body embeds
// the SHA directly and must resolve without ever reaching the API-backed
// fallback, even when an app is configured.
func TestResolveRevertedSHAPrefersCommitMessagePattern(t *testing.T) {
	srv := githubAppFixture(t)
	wantSHA := strings.Repeat("c", 40)

	var payload githubWebhookPayload
	payload.PullRequest.Body = "This reverts commit " + wantSHA + "."
	inst := &store.GitHubInstallation{InstallationID: testGitHubInstallationID, OrgID: "org1"}

	sha, ok := srv.resolveRevertedSHA(context.Background(), payload, inst, "http://unused.invalid")
	if !ok {
		t.Fatal("resolveRevertedSHA returned ok=false, want true")
	}
	if sha != wantSHA {
		t.Errorf("sha = %q, want %q", sha, wantSHA)
	}
}
