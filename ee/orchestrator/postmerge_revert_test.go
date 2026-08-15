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

func revertPRPayload(revertedSHA string) []byte {
	payload := map[string]any{
		"action": "opened",
		"pull_request": map[string]any{
			"number":   43,
			"title":    `Revert "add a health endpoint"`,
			"body":     "This reverts commit " + revertedSHA + ".",
			"html_url": "https://github.com/acme/widgets/pull/43",
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
