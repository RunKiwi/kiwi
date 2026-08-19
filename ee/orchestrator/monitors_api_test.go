// SPDX-License-Identifier: LicenseRef-Kiwi-BSL-1.1
// Copyright (c) 2026 RunKiwi. Licensed under the Business Source License 1.1.
// See ee/LICENSE. This is Control Plane code and is NOT Apache-2.0.

package orchestrator

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/ibreakthecloud/kiwi/ee/auth"
	"github.com/ibreakthecloud/kiwi/pkg/store"
)

// redirectTransport rewrites every outbound request's scheme/host to point
// at a local httptest server, so handleCreateMonitor's production call
// (which always dials the real githubAPIDefault, per createExternalMonitor's
// api parameter) can be exercised in a test without a real network call.
// githubCallClient (github_pr_calls.go) is a package-level var precisely so
// something like this can swap its Transport out.
type redirectTransport struct{ target *url.URL }

func (rt redirectTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.URL.Scheme = rt.target.Scheme
	req.URL.Host = rt.target.Host
	req.Host = rt.target.Host
	return http.DefaultTransport.RoundTrip(req)
}

// withGithubAPIRedirect points githubCallClient at a local fixture server
// for the duration of the test and restores it on cleanup.
func withGithubAPIRedirect(t *testing.T, targetURL string) {
	t.Helper()
	target, err := url.Parse(targetURL)
	if err != nil {
		t.Fatal(err)
	}
	orig := githubCallClient.Transport
	githubCallClient.Transport = redirectTransport{target: target}
	t.Cleanup(func() { githubCallClient.Transport = orig })
}

func TestHandleCreateMonitorFromPRURL(t *testing.T) {
	srv, s := setupWebhookTest(t)
	orgID := "org1"
	if err := s.DB().Create(&store.Organization{ID: orgID, Name: "acme"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.DB().Create(&store.GitHubInstallation{
		InstallationID: testGitHubInstallationID, OrgID: orgID, AccountLogin: "acme",
	}).Error; err != nil {
		t.Fatal(err)
	}

	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"merged":true,"merge_commit_sha":"` + strings.Repeat("e", 40) + `"}`))
	}))
	defer api.Close()
	withGithubAPIRedirect(t, api.URL)
	srv.githubApp = githubAppFixture(t).githubApp

	body, _ := json.Marshal(map[string]string{"pr_url": "https://github.com/acme/widgets/pull/99"})
	r := httptest.NewRequest(http.MethodPost, "/api/v1/monitors", bytes.NewReader(body))
	r = r.WithContext(auth.ContextWithClaims(r.Context(), &auth.UserClaims{OrgID: orgID}))
	rec := httptest.NewRecorder()
	srv.handleCreateMonitor(rec, r)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var mon store.PostMergeMonitor
	if err := json.Unmarshal(rec.Body.Bytes(), &mon); err != nil {
		t.Fatal(err)
	}
	if mon.Origin != store.MonitorOriginExternalPR {
		t.Errorf("origin = %q, want external_pr", mon.Origin)
	}
	if mon.Repo != "acme/widgets" || mon.PRNumber != 99 {
		t.Errorf("repo/pr = %q/%d, want acme/widgets/99", mon.Repo, mon.PRNumber)
	}
}

func TestHandleCreateMonitorRejectsUnmergedPR(t *testing.T) {
	srv, s := setupWebhookTest(t)
	orgID := "org1"
	if err := s.DB().Create(&store.Organization{ID: orgID, Name: "acme"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.DB().Create(&store.GitHubInstallation{
		InstallationID: testGitHubInstallationID, OrgID: orgID, AccountLogin: "acme",
	}).Error; err != nil {
		t.Fatal(err)
	}

	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"merged":false}`))
	}))
	defer api.Close()
	withGithubAPIRedirect(t, api.URL)
	srv.githubApp = githubAppFixture(t).githubApp

	body, _ := json.Marshal(map[string]string{"pr_url": "https://github.com/acme/widgets/pull/99"})
	r := httptest.NewRequest(http.MethodPost, "/api/v1/monitors", bytes.NewReader(body))
	r = r.WithContext(auth.ContextWithClaims(r.Context(), &auth.UserClaims{OrgID: orgID}))
	rec := httptest.NewRecorder()
	srv.handleCreateMonitor(rec, r)

	if rec.Code < 400 || rec.Code >= 500 {
		t.Fatalf("status = %d, want 4xx", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "merged") {
		t.Errorf("body = %q, want it to mention \"merged\"", rec.Body.String())
	}
}

func TestHandleCreateMonitorRejectsBadURL(t *testing.T) {
	srv, _ := setupWebhookTest(t)
	body, _ := json.Marshal(map[string]string{"pr_url": "not a pr url"})
	r := httptest.NewRequest(http.MethodPost, "/api/v1/monitors", bytes.NewReader(body))
	r = r.WithContext(auth.ContextWithClaims(r.Context(), &auth.UserClaims{OrgID: "org1"}))
	rec := httptest.NewRecorder()
	srv.handleCreateMonitor(rec, r)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestHandleListMonitorsScopesToOrg(t *testing.T) {
	srv, s := setupWebhookTest(t)
	if err := s.DB().Create(&store.Organization{ID: "org1", Name: "a"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.DB().Create(&store.Organization{ID: "org2", Name: "b"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.CreateMonitor(context.Background(), &store.PostMergeMonitor{
		ID: "mon_1", OrgID: "org1", JobID: "job1", Origin: store.MonitorOriginKiwiPR,
		Repo: "acme/widgets", PRNumber: 1, MergeCommitSHA: "a", Status: store.MonitorStatusMonitoring,
		DeployedAt: time.Now(), WindowEndsAt: time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateMonitor(context.Background(), &store.PostMergeMonitor{
		ID: "mon_2", OrgID: "org2", JobID: "job2", Origin: store.MonitorOriginKiwiPR,
		Repo: "other/repo", PRNumber: 1, MergeCommitSHA: "b", Status: store.MonitorStatusMonitoring,
		DeployedAt: time.Now(), WindowEndsAt: time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatal(err)
	}

	r := httptest.NewRequest(http.MethodGet, "/api/v1/monitors", nil)
	r = r.WithContext(auth.ContextWithClaims(r.Context(), &auth.UserClaims{OrgID: "org1"}))
	rec := httptest.NewRecorder()
	srv.handleListMonitors(rec, r)

	var out struct {
		Monitors []store.PostMergeMonitor `json:"monitors"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Monitors) != 1 || out.Monitors[0].ID != "mon_1" {
		t.Fatalf("got %+v, want exactly mon_1", out.Monitors)
	}
}

func TestHandleCancelMonitor(t *testing.T) {
	srv, s := setupWebhookTest(t)
	if err := s.DB().Create(&store.Organization{ID: "org1", Name: "a"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.CreateMonitor(context.Background(), &store.PostMergeMonitor{
		ID: "mon_1", OrgID: "org1", JobID: "job1", Origin: store.MonitorOriginKiwiPR,
		Repo: "acme/widgets", PRNumber: 1, MergeCommitSHA: "a", Status: store.MonitorStatusMonitoring,
		DeployedAt: time.Now(), WindowEndsAt: time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatal(err)
	}

	r := httptest.NewRequest(http.MethodPost, "/api/v1/monitors/mon_1/cancel", nil)
	r = r.WithContext(auth.ContextWithClaims(r.Context(), &auth.UserClaims{OrgID: "org1"}))
	rec := httptest.NewRecorder()
	srv.handleCancelMonitor(rec, r)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	got, err := s.GetMonitorByID(context.Background(), "mon_1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != store.MonitorStatusCancelled {
		t.Errorf("status = %q, want CANCELLED", got.Status)
	}
}

func TestHandleCancelMonitorRejectsOtherOrg(t *testing.T) {
	srv, s := setupWebhookTest(t)
	if err := s.DB().Create(&store.Organization{ID: "org1", Name: "a"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.DB().Create(&store.Organization{ID: "org2", Name: "b"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.CreateMonitor(context.Background(), &store.PostMergeMonitor{
		ID: "mon_1", OrgID: "org1", JobID: "job1", Origin: store.MonitorOriginKiwiPR,
		Repo: "acme/widgets", PRNumber: 1, MergeCommitSHA: "a", Status: store.MonitorStatusMonitoring,
		DeployedAt: time.Now(), WindowEndsAt: time.Now().Add(time.Hour),
	}); err != nil {
		t.Fatal(err)
	}

	r := httptest.NewRequest(http.MethodPost, "/api/v1/monitors/mon_1/cancel", nil)
	r = r.WithContext(auth.ContextWithClaims(r.Context(), &auth.UserClaims{OrgID: "org2"}))
	rec := httptest.NewRecorder()
	srv.handleCancelMonitor(rec, r)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	got, err := s.GetMonitorByID(context.Background(), "mon_1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != store.MonitorStatusMonitoring {
		t.Errorf("status = %q, want unchanged MONITORING", got.Status)
	}
}

func TestHandleCancelMonitorRejectsBadPath(t *testing.T) {
	srv, _ := setupWebhookTest(t)
	r := httptest.NewRequest(http.MethodPost, "/api/v1/monitors/mon_1", nil)
	r = r.WithContext(auth.ContextWithClaims(r.Context(), &auth.UserClaims{OrgID: "org1"}))
	rec := httptest.NewRecorder()
	srv.handleCancelMonitor(rec, r)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}
