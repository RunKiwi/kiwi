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

func checkRunPayload(action, conclusion, sha string) []byte {
	payload := map[string]any{
		"action": action,
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
