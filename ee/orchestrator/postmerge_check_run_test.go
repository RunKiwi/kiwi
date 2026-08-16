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
	"testing"
	"time"

	"github.com/ibreakthecloud/kiwi/pkg/store"
)

// checkRunPayload builds a check_run webhook body against the
// "acme/widgets" repo — the repo seedMonitorWithRecord seeds its QueuedTask
// and monitor under, so this resolves to org1 the same way checkForRevert's
// org resolution does.
func checkRunPayload(action, conclusion, sha string) []byte {
	return checkRunPayloadForRepo(action, conclusion, sha, "acme", "widgets")
}

func checkRunPayloadForRepo(action, conclusion, sha, owner, repo string) []byte {
	payload := map[string]any{
		"action": action,
		"check_run": map[string]any{
			"head_sha":   sha,
			"conclusion": conclusion,
		},
		"repository": map[string]any{"name": repo, "owner": map[string]any{"login": owner}},
	}
	b, _ := json.Marshal(payload)
	return b
}

func postCheckRun(t *testing.T, srv *Server, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/github", bytes.NewReader(body))
	req.Header.Set("X-GitHub-Event", "check_run")
	req.Header.Set("X-Hub-Signature-256", generateSignature([]byte(commentWebhookSecret), body))
	rec := httptest.NewRecorder()
	srv.handleGithubWebhook(rec, req)
	return rec
}

func TestFailedCheckRunFinalizesMonitorAsRegression(t *testing.T) {
	withCommentSecret(t)
	srv, s := setupWebhookTest(t)
	mon := seedMonitorWithRecord(t, s, "org1", "job1", false)

	rec := postCheckRun(t, srv, checkRunPayload("completed", "failure", mon.MergeCommitSHA))
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

func TestTimedOutCheckRunFinalizesMonitorAsRegression(t *testing.T) {
	withCommentSecret(t)
	srv, s := setupWebhookTest(t)
	mon := seedMonitorWithRecord(t, s, "org1", "job1", false)

	rec := postCheckRun(t, srv, checkRunPayload("completed", "timed_out", mon.MergeCommitSHA))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var got store.PostMergeMonitor
	if err := s.DB().First(&got, "id = ?", mon.ID).Error; err != nil {
		t.Fatal(err)
	}
	if got.Status != store.MonitorStatusRegression {
		t.Errorf("status = %q, want REGRESSION on a timed_out conclusion", got.Status)
	}
}

func TestSuccessfulCheckRunDoesNotFinalize(t *testing.T) {
	withCommentSecret(t)
	srv, s := setupWebhookTest(t)
	mon := seedMonitorWithRecord(t, s, "org1", "job1", false)

	rec := postCheckRun(t, srv, checkRunPayload("completed", "success", mon.MergeCommitSHA))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var got store.PostMergeMonitor
	if err := s.DB().First(&got, "id = ?", mon.ID).Error; err != nil {
		t.Fatal(err)
	}
	if got.Status != store.MonitorStatusMonitoring {
		t.Errorf("status = %q, want unchanged MONITORING on a successful check run", got.Status)
	}
}

func TestInProgressCheckRunDoesNotFinalize(t *testing.T) {
	withCommentSecret(t)
	srv, s := setupWebhookTest(t)
	mon := seedMonitorWithRecord(t, s, "org1", "job1", false)

	rec := postCheckRun(t, srv, checkRunPayload("in_progress", "", mon.MergeCommitSHA))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var got store.PostMergeMonitor
	if err := s.DB().First(&got, "id = ?", mon.ID).Error; err != nil {
		t.Fatal(err)
	}
	if got.Status != store.MonitorStatusMonitoring {
		t.Errorf("status = %q, want unchanged MONITORING while the check is still running", got.Status)
	}
}

// TestCheckRunOrgResolutionDoesNotMatchAcrossUnderscoreRepoNames covers the
// LIKE-escaping fix: an unescaped "_" in a repo name is a SQL wildcard for
// "any one character", so a QueuedTask.ResultURL for an unrelated repo whose
// name differs only by having some other character where the tracked repo
// has an underscore (e.g. "myXrepo" vs. "my_repo") must NOT resolve as a
// match — that would let a check run on a different repo entirely finalize
// this org's monitor.
func TestCheckRunOrgResolutionDoesNotMatchAcrossUnderscoreRepoNames(t *testing.T) {
	withCommentSecret(t)
	srv, s := setupWebhookTest(t)
	ctx := context.Background()

	// A repo whose name would, unescaped, be matched by the LIKE pattern
	// built from "acme/my_repo" below (the "_" wildcards to "X").
	if err := s.DB().Create(&store.Organization{ID: "orgX", Name: "orgX"}).Error; err != nil {
		t.Fatal(err)
	}
	prURL := "https://github.com/acme/myXrepo/pull/1"
	if err := s.EnqueueTask(ctx, &store.QueuedTask{
		ID: "qtX", OrgID: "orgX", JobID: "jobX", RootTaskID: "qtX",
		Status: store.TaskSucceeded, ResultURL: &prURL,
	}); err != nil {
		t.Fatal(err)
	}
	monX := &store.PostMergeMonitor{
		ID: "mon_x", OrgID: "orgX", JobID: "jobX", Repo: "acme/myXrepo", PRNumber: 1,
		MergeCommitSHA: "1111111111111111111111111111111111111111", Status: store.MonitorStatusMonitoring,
		DeployedAt: time.Now(), WindowEndsAt: time.Now().Add(24 * time.Hour),
	}
	if err := s.CreateMonitor(ctx, monX); err != nil {
		t.Fatal(err)
	}

	// A failed check run on a DIFFERENT, real repo containing an underscore.
	// No org owns "acme/my_repo" in this test at all — the point is that this
	// must not spuriously resolve to orgX's monitor via the wildcard.
	body := checkRunPayloadForRepo("completed", "failure", monX.MergeCommitSHA, "acme", "my_repo")
	rec := postCheckRun(t, srv, body)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var got store.PostMergeMonitor
	if err := s.DB().First(&got, "id = ?", "mon_x").Error; err != nil {
		t.Fatal(err)
	}
	if got.Status != store.MonitorStatusMonitoring {
		t.Errorf("status = %q, want unchanged MONITORING — a check run on a different repo must not match via an unescaped LIKE wildcard", got.Status)
	}
}

// TestFailedCheckRunFromUnknownRepoDoesNotFinalize covers the org-resolution
// miss: a failed check run whose repository has no QueuedTask with a
// matching result_url (so no org can be resolved) must no-op cleanly —
// no panic, and no monitor in any org gets touched.
func TestFailedCheckRunFromUnknownRepoDoesNotFinalize(t *testing.T) {
	withCommentSecret(t)
	srv, s := setupWebhookTest(t)
	mon := seedMonitorWithRecord(t, s, "org1", "job1", false)

	body := checkRunPayloadForRepo("completed", "failure", mon.MergeCommitSHA, "someone", "else")
	rec := postCheckRun(t, srv, body)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var got store.PostMergeMonitor
	if err := s.DB().First(&got, "id = ?", mon.ID).Error; err != nil {
		t.Fatal(err)
	}
	if got.Status != store.MonitorStatusMonitoring {
		t.Errorf("status = %q, want unchanged MONITORING when the reporting repo resolves to no org", got.Status)
	}
}
