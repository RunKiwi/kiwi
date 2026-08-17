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
