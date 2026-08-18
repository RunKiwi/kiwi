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
