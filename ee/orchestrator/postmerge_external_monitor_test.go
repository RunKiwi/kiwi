// SPDX-License-Identifier: LicenseRef-Kiwi-BSL-1.1
// Copyright (c) 2026 RunKiwi. Licensed under the Business Source License 1.1.
// See ee/LICENSE. This is Control Plane code and is NOT Apache-2.0.

package orchestrator

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ibreakthecloud/kiwi/pkg/provider"
	"github.com/ibreakthecloud/kiwi/pkg/store"
)

func TestCreateExternalMonitorFromAMergedPR(t *testing.T) {
	srv, s := setupWebhookTest(t) // reuse Phase 1a's existing helper
	orgID := "org1"
	if err := s.DB().Create(&store.Organization{ID: orgID, Name: "acme"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.DB().Create(&store.GitHubInstallation{
		InstallationID: testGitHubInstallationID, OrgID: orgID, AccountLogin: "acme",
	}).Error; err != nil {
		t.Fatal(err)
	}
	// Wire srv.githubApp to a fixture that mints a token, then point
	// getPullRequest at a local httptest server by passing its URL directly
	// as createExternalMonitor's api parameter.
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"merged":true,"merge_commit_sha":"` + strings.Repeat("e", 40) + `"}`))
	}))
	defer api.Close()
	srv.githubApp = githubAppFixture(t).githubApp

	mon, err := srv.createExternalMonitor(context.Background(), orgID, "acme", "widgets", 99, api.URL)
	if err != nil {
		t.Fatal(err)
	}
	if mon.Origin != store.MonitorOriginExternalPR {
		t.Errorf("origin = %q, want external_pr", mon.Origin)
	}
	if mon.JobID != "" {
		t.Errorf("job_id = %q, want empty", mon.JobID)
	}
}

func TestCreateExternalMonitorRejectsUnmergedPR(t *testing.T) {
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
	srv.githubApp = githubAppFixture(t).githubApp

	_, err := srv.createExternalMonitor(context.Background(), orgID, "acme", "widgets", 99, api.URL)
	if !errors.Is(err, ErrPRNotMerged) {
		t.Errorf("err = %v, want ErrPRNotMerged", err)
	}
}

// TestCreateExternalMonitorAllowsMultiplePerOrg guards the property Task 1's
// migration 0042 exists to provide: every external monitor is created with
// JobID: "", so a plain (org_id, job_id) unique index — the shape Phase 1a
// shipped with in migration 0037 — would make the second external monitor
// for an org collide with the first. Migration 0042 replaces it with a
// partial index (WHERE job_id != ”), which exempts empty-JobID rows.
//
// setupWebhookTest (github_webhook_test.go) builds its schema via
// db.AutoMigrate(&store.PostMergeMonitor{}, ...) against SQLite, and GORM's
// index struct tags have no way to express a partial/WHERE condition — so
// AutoMigrate alone produces the old, non-partial unique index regardless of
// what the tag says. setupWebhookTest drops and recreates that index with
// the WHERE clause by raw SQL immediately after migrating, exactly like
// newTestStore does in pkg/store/store_test.go, which is what lets this test
// run for real instead of needing a skip. pkg/store/postmerge_monitor_test.go's
// TestCreateMonitorWithoutJobIDIsAnExternalPRMonitor already proves the same
// property at the store layer; this one additionally exercises it through
// createExternalMonitor's full path (PR resolution, merge check, duplicate
// lookup, row creation).
func TestCreateExternalMonitorAllowsMultiplePerOrg(t *testing.T) {
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
		sha := strings.Repeat("e", 40)
		if strings.HasSuffix(r.URL.Path, "/100") {
			sha = strings.Repeat("f", 40)
		}
		_, _ = w.Write([]byte(`{"merged":true,"merge_commit_sha":"` + sha + `"}`))
	}))
	defer api.Close()
	srv.githubApp = githubAppFixture(t).githubApp

	if _, err := srv.createExternalMonitor(context.Background(), orgID, "acme", "widgets", 99, api.URL); err != nil {
		t.Fatalf("first monitor: %v", err)
	}
	if _, err := srv.createExternalMonitor(context.Background(), orgID, "acme", "widgets", 100, api.URL); err != nil {
		t.Fatalf("second monitor (same org, empty job_id): %v", err)
	}
}

// TestCreateExternalMonitorRejectsDuplicateSHA is the only test exercising
// ErrMonitorAlreadyExists — the sentinel Tasks 4 and 6 both need to
// distinguish from ErrPRNotMerged for their own error messages.
func TestCreateExternalMonitorRejectsDuplicateSHA(t *testing.T) {
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
	sha := strings.Repeat("e", 40)
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"merged":true,"merge_commit_sha":"` + sha + `"}`))
	}))
	defer api.Close()
	srv.githubApp = githubAppFixture(t).githubApp

	if _, err := srv.createExternalMonitor(context.Background(), orgID, "acme", "widgets", 99, api.URL); err != nil {
		t.Fatalf("first monitor: %v", err)
	}
	_, err := srv.createExternalMonitor(context.Background(), orgID, "acme", "widgets", 100, api.URL)
	if !errors.Is(err, ErrMonitorAlreadyExists) {
		t.Errorf("err = %v, want ErrMonitorAlreadyExists", err)
	}
}

// TestCreateExternalMonitorEnqueuesTelemetryPolls proves Finding 1's fix: an
// external_pr monitor — one created from a PR-comment or dashboard request
// rather than Phase 1a's merge webhook — must get the same telemetry
// regression detection a Kiwi-authored monitor gets. Before this fix,
// createExternalMonitor returned after CreateMonitor without ever calling
// enqueueTelemetryPolls, so no postmerge_telemetry_polls row was ever written
// for one of these monitors, contradicting this branch's own README claim
// that such a monitor "gets the same GitHub-native and telemetry regression
// detection... as a Kiwi-authored one."
//
// Mirrors TestEnqueueTelemetryPollsCreatesAPollWhenAMetricIsConfigured
// (postmerge_telemetry_test.go), but drives the whole thing through
// createExternalMonitor instead of calling enqueueTelemetryPolls directly,
// so the fixture's "title" field is at least decoded and threaded through as
// the intent argument (TestGetPullRequestReadsMergeOutcome, in
// github_pr_calls_test.go, is what actually pins the decode itself — this
// test's MockMetricSelector ignores the intent string, so it does not by
// itself prove the title's contents reach metric selection).
func TestCreateExternalMonitorEnqueuesTelemetryPolls(t *testing.T) {
	srv, s := setupWebhookTest(t)
	ctx := context.Background()
	orgID := "org1"
	if err := s.DB().Create(&store.Organization{ID: orgID, Name: "acme"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.DB().Create(&store.GitHubInstallation{
		InstallationID: testGitHubInstallationID, OrgID: orgID, AccountLogin: "acme",
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.CreateTelemetryMetric(ctx, &store.TelemetryMetric{
		ID: "tm_1", OrgID: orgID, Repo: "acme/widgets", Name: "checkout_p95_latency",
		Provider: "datadog", Query: "p95:trace.checkout{env:prod}", ComparisonDirection: store.ComparisonLowerIsBetter,
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveCredential(ctx, orgID, "DATADOG_API_KEY", store.CredentialTelemetry, "dd-api-key"); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveCredential(ctx, orgID, "DATADOG_APP_KEY", store.CredentialTelemetry, "dd-app-key"); err != nil {
		t.Fatal(err)
	}
	srv.metricSelector = &provider.MockMetricSelector{Choice: "checkout_p95_latency"}

	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"merged":true,"merge_commit_sha":"` + strings.Repeat("e", 40) + `","title":"speed up checkout"}`))
	}))
	defer api.Close()
	srv.githubApp = githubAppFixture(t).githubApp

	mon, err := srv.createExternalMonitor(ctx, orgID, "acme", "widgets", 99, api.URL)
	if err != nil {
		t.Fatal(err)
	}

	var polls []store.PostMergeTelemetryPoll
	if err := s.DB().Where("monitor_id = ?", mon.ID).Find(&polls).Error; err != nil {
		t.Fatal(err)
	}
	if len(polls) != 1 {
		t.Fatalf("got %d telemetry polls for the external_pr monitor, want 1", len(polls))
	}
	if polls[0].Query != "p95:trace.checkout{env:prod}" {
		t.Errorf("query = %q", polls[0].Query)
	}
}
