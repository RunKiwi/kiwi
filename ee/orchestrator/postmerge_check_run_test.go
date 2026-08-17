// SPDX-License-Identifier: LicenseRef-Kiwi-BSL-1.1
// Copyright (c) 2026 RunKiwi. Licensed under the Business Source License 1.1.
// See ee/LICENSE. This is Control Plane code and is NOT Apache-2.0.

package orchestrator

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ibreakthecloud/kiwi/pkg/store"
)

// checkRunPayload builds a check_run webhook body carrying
// testGitHubInstallationID — the id seedMonitorWithRecord links to org1, and
// the only signal handleCheckRun uses to resolve which org's monitor to
// finalize (see handleCheckRun's comment for why it's not repo-name-based).
func checkRunPayload(action, conclusion, sha string) []byte {
	return checkRunPayloadForInstallation(action, conclusion, sha, testGitHubInstallationID)
}

func checkRunPayloadForInstallation(action, conclusion, sha string, installationID int64) []byte {
	payload := map[string]any{
		"action":       action,
		"installation": map[string]any{"id": installationID},
		"check_run": map[string]any{
			"head_sha":   sha,
			"conclusion": conclusion,
		},
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

// TestActionRequiredCheckRunFinalizesMonitorAsRegression closes a coverage
// gap: handleCheckRun's switch treats failure, timed_out, and action_required
// as regressions, but only failure and timed_out had a test proving it. A
// future edit narrowing the switch could silently drop action_required
// without any test catching it.
func TestActionRequiredCheckRunFinalizesMonitorAsRegression(t *testing.T) {
	withCommentSecret(t)
	srv, s := setupWebhookTest(t)
	mon := seedMonitorWithRecord(t, s, "org1", "job1", false)

	rec := postCheckRun(t, srv, checkRunPayload("completed", "action_required", mon.MergeCommitSHA))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var got store.PostMergeMonitor
	if err := s.DB().First(&got, "id = ?", mon.ID).Error; err != nil {
		t.Fatal(err)
	}
	if got.Status != store.MonitorStatusRegression {
		t.Errorf("status = %q, want REGRESSION on an action_required conclusion", got.Status)
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

// TestFailedCheckRunFromUnknownInstallationDoesNotFinalize covers the
// org-resolution miss: a failed check run whose installation id has no
// GitHubInstallation row on file (so no org can be resolved) must no-op
// cleanly — no panic, and no monitor in any org gets touched. Org resolution
// is installation-id-based, not repo-name-based (see handleCheckRun's
// comment), so "unknown" here means an unrecognized installation id, not an
// unrecognized repo name.
func TestFailedCheckRunFromUnknownInstallationDoesNotFinalize(t *testing.T) {
	withCommentSecret(t)
	srv, s := setupWebhookTest(t)
	mon := seedMonitorWithRecord(t, s, "org1", "job1", false)

	body := checkRunPayloadForInstallation("completed", "failure", mon.MergeCommitSHA, testGitHubInstallationID+1)
	rec := postCheckRun(t, srv, body)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var got store.PostMergeMonitor
	if err := s.DB().First(&got, "id = ?", mon.ID).Error; err != nil {
		t.Fatal(err)
	}
	if got.Status != store.MonitorStatusMonitoring {
		t.Errorf("status = %q, want unchanged MONITORING when the reporting installation resolves to no org", got.Status)
	}
}
