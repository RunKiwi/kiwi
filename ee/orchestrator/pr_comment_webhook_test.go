// SPDX-License-Identifier: LicenseRef-Kiwi-BSL-1.1
// Copyright (c) 2026 RunKiwi. Licensed under the Business Source License 1.1.
// See ee/LICENSE. This is Control Plane code and is NOT Apache-2.0.

package orchestrator

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/ibreakthecloud/kiwi/pkg/store"
)

const commentWebhookSecret = "comment-secret"

// commentDelivery builds an issue_comment payload for a pull request.
func commentDelivery(body, assoc, senderType string, commentID int64) []byte {
	payload := map[string]any{
		"action": "created",
		"issue": map[string]any{
			"number":       42,
			"state":        "open",
			"html_url":     "https://github.com/acme/widgets/pull/42",
			"pull_request": map[string]any{"url": "https://api.github.com/repos/acme/widgets/pulls/42"},
		},
		"comment": map[string]any{
			"id":                 commentID,
			"body":               body,
			"author_association": assoc,
		},
		"repository": map[string]any{"name": "widgets", "owner": map[string]any{"login": "acme"}},
		"sender":     map[string]any{"login": "reviewer", "type": senderType},
	}
	b, _ := json.Marshal(payload)
	return b
}

// seedCommentTask installs a finished task whose result_url is the PR the
// comments below are left on.
func seedCommentTask(t *testing.T, s *store.PostgresStore, mode string) *store.QueuedTask {
	t.Helper()
	ctx := context.Background()

	if err := s.DB().Create(&store.Organization{ID: "org1", Name: "acme", PRCommentMode: mode}).Error; err != nil {
		t.Fatal(err)
	}
	prURL := "https://github.com/acme/widgets/pull/42"
	task := &store.QueuedTask{
		ID: "job_abc-impl", OrgID: "org1", JobID: "job_abc",
		Status: store.TaskSucceeded, ResultURL: &prURL,
		Spec: map[string]interface{}{
			"task": "add a health endpoint", "model": "claude-sonnet-5",
			"test_cmd": "go test ./...", "repo_url": "https://github.com/acme/widgets",
			"mode": "session",
		},
	}
	if err := s.EnqueueTask(ctx, task); err != nil {
		t.Fatal(err)
	}
	return task
}

func postComment(t *testing.T, srv *Server, event string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/github", bytes.NewReader(body))
	req.Header.Set("X-GitHub-Event", event)
	req.Header.Set("X-Hub-Signature-256", generateSignature([]byte(commentWebhookSecret), body))
	rec := httptest.NewRecorder()
	srv.handleGithubWebhook(rec, req)
	return rec
}

func continuationsFor(t *testing.T, s *store.PostgresStore, root string) []store.QueuedTask {
	t.Helper()
	all, err := s.ThreadTasks(context.Background(), "org1", root)
	if err != nil {
		t.Fatal(err)
	}
	out := make([]store.QueuedTask, 0, len(all))
	for _, task := range all {
		if task.Origin == store.OriginPRComment {
			out = append(out, task)
		}
	}
	return out
}

func withCommentSecret(t *testing.T) {
	t.Helper()
	t.Setenv("GITHUB_WEBHOOK_SECRET", commentWebhookSecret)
	_ = os.Getenv("GITHUB_WEBHOOK_SECRET")
}

// The happy path: a collaborator mentions Kiwi on an open PR that Kiwi opened.
func TestCommentTriggerEnqueuesAContinuation(t *testing.T) {
	withCommentSecret(t)
	srv, s := setupWebhookTest(t)
	parent := seedCommentTask(t, s, store.PRCommentModeMention)

	rec := postComment(t, srv, "issue_comment",
		commentDelivery("@runkiwi rename the handler", "COLLABORATOR", "User", 111))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	got := continuationsFor(t, s, parent.ID)
	if len(got) != 1 {
		t.Fatalf("got %d continuations, want 1", len(got))
	}
	if got[0].JobID != parent.JobID {
		t.Errorf("job id = %q, want the parent's %q — the PR would not update", got[0].JobID, parent.JobID)
	}
	if got[0].Spec["task"] != "rename the handler" {
		t.Errorf("task = %v, want the instruction with the mention stripped", got[0].Spec["task"])
	}
}

// Every guard below must return 200 and enqueue nothing. A non-2xx teaches
// GitHub to disable the hook, which would take the merge records down with it.
func TestCommentTriggerGuards(t *testing.T) {
	for _, tc := range []struct {
		name  string
		mode  string
		body  []byte
		event string
	}{
		{
			name: "kiwi's own reply must never trigger a round",
			mode: store.PRCommentModeAny,
			body: commentDelivery("@runkiwi pushed round 3", "NONE", "Bot", 201),
		},
		{
			name: "mode off ignores even a direct mention",
			mode: store.PRCommentModeOff,
			body: commentDelivery("@runkiwi do it", "OWNER", "User", 202),
		},
		{
			name: "mode mention ignores a comment that does not address Kiwi",
			mode: store.PRCommentModeMention,
			body: commentDelivery("looks good to me", "OWNER", "User", 203),
		},
		{
			name: "a drive-by commenter cannot spend the org's minutes",
			mode: store.PRCommentModeAny,
			body: commentDelivery("@runkiwi rewrite everything", "CONTRIBUTOR", "User", 204),
		},
		{
			name: "an empty instruction has nothing to act on",
			mode: store.PRCommentModeMention,
			body: commentDelivery("@runkiwi", "OWNER", "User", 205),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			withCommentSecret(t)
			srv, s := setupWebhookTest(t)
			parent := seedCommentTask(t, s, tc.mode)

			event := tc.event
			if event == "" {
				event = "issue_comment"
			}
			rec := postComment(t, srv, event, tc.body)
			if rec.Code != http.StatusOK {
				t.Errorf("status = %d, want 200", rec.Code)
			}
			if got := continuationsFor(t, s, parent.ID); len(got) != 0 {
				t.Errorf("got %d continuations, want none", len(got))
			}
		})
	}
}

// A comment on a pull request Kiwi did not open is not ours to act on.
func TestCommentOnAnUnknownPullRequestIsIgnored(t *testing.T) {
	withCommentSecret(t)
	srv, s := setupWebhookTest(t)
	seedCommentTask(t, s, store.PRCommentModeAny)

	payload := map[string]any{
		"action": "created",
		"issue": map[string]any{
			"number": 9, "state": "open",
			"html_url":     "https://github.com/someone/else/pull/9",
			"pull_request": map[string]any{"url": "x"},
		},
		"comment":    map[string]any{"id": 301, "body": "@runkiwi hello", "author_association": "OWNER"},
		"repository": map[string]any{"name": "else", "owner": map[string]any{"login": "someone"}},
		"sender":     map[string]any{"login": "u", "type": "User"},
	}
	body, _ := json.Marshal(payload)

	rec := postComment(t, srv, "issue_comment", body)
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}

// A merged pull request's branch is spent. Continuing onto it would push to a
// branch nobody is reviewing.
func TestCommentOnAClosedPullRequestIsIgnored(t *testing.T) {
	withCommentSecret(t)
	srv, s := setupWebhookTest(t)
	parent := seedCommentTask(t, s, store.PRCommentModeAny)

	payload := map[string]any{
		"action": "created",
		"issue": map[string]any{
			"number": 42, "state": "closed",
			"html_url":     "https://github.com/acme/widgets/pull/42",
			"pull_request": map[string]any{"url": "x"},
		},
		"comment":    map[string]any{"id": 401, "body": "@runkiwi one more thing", "author_association": "OWNER"},
		"repository": map[string]any{"name": "widgets", "owner": map[string]any{"login": "acme"}},
		"sender":     map[string]any{"login": "u", "type": "User"},
	}
	body, _ := json.Marshal(payload)

	rec := postComment(t, srv, "issue_comment", body)
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
	if got := continuationsFor(t, s, parent.ID); len(got) != 0 {
		t.Errorf("got %d continuations for a closed PR, want none", len(got))
	}
}

// GitHub redelivers. The second delivery must be a no-op rather than a second
// round billed to the customer.
func TestRedeliveredCommentDoesNotBuyASecondRound(t *testing.T) {
	withCommentSecret(t)
	srv, s := setupWebhookTest(t)
	parent := seedCommentTask(t, s, store.PRCommentModeMention)

	body := commentDelivery("@runkiwi rename the handler", "OWNER", "User", 501)
	for i := 0; i < 2; i++ {
		if rec := postComment(t, srv, "issue_comment", body); rec.Code != http.StatusOK {
			t.Fatalf("delivery %d: status = %d, want 200", i+1, rec.Code)
		}
	}

	if got := continuationsFor(t, s, parent.ID); len(got) != 1 {
		t.Fatalf("got %d continuations after a redelivery, want 1", len(got))
	}
}

// Two tasks in one thread share a branch and both force-push to it, so the
// loser's work would vanish with no error raised anywhere.
func TestSecondCommentWhileWorkIsRunningIsRefused(t *testing.T) {
	withCommentSecret(t)
	srv, s := setupWebhookTest(t)
	parent := seedCommentTask(t, s, store.PRCommentModeMention)

	first := postComment(t, srv, "issue_comment",
		commentDelivery("@runkiwi rename the handler", "OWNER", "User", 601))
	if first.Code != http.StatusOK {
		t.Fatalf("status = %d", first.Code)
	}
	second := postComment(t, srv, "issue_comment",
		commentDelivery("@runkiwi and add a test", "OWNER", "User", 602))
	if second.Code != http.StatusOK {
		t.Fatalf("status = %d", second.Code)
	}

	if got := continuationsFor(t, s, parent.ID); len(got) != 1 {
		t.Fatalf("got %d continuations, want 1 — the thread already had work in flight", len(got))
	}
}

// The session travels to the continuation, which is what makes it a resume
// rather than a fresh run that happens to share a branch.
func TestContinuationTakesOverTheSession(t *testing.T) {
	withCommentSecret(t)
	srv, s := setupWebhookTest(t)
	parent := seedCommentTask(t, s, store.PRCommentModeMention)
	ctx := context.Background()

	sess := &store.AgentSession{
		ID: "sess_" + parent.ID, OrgID: "org1", TaskID: parent.ID, JobID: parent.JobID,
		Round: 3, BaseSHA: "base", HeadSHA: "head",
		State: map[string]interface{}{"round": 3},
	}
	if err := s.SaveAgentSession(ctx, sess, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.FinishAgentSession(ctx, "org1", sess.ID, store.SessionSucceeded); err != nil {
		t.Fatal(err)
	}

	if rec := postComment(t, srv, "issue_comment",
		commentDelivery("@runkiwi rename the handler", "OWNER", "User", 701)); rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}

	got := continuationsFor(t, s, parent.ID)
	if len(got) != 1 {
		t.Fatalf("got %d continuations, want 1", len(got))
	}
	moved, err := s.GetAgentSessionByTask(ctx, "org1", got[0].ID)
	if err != nil {
		t.Fatalf("the session did not move to the continuation: %v", err)
	}
	if moved.ID != sess.ID {
		t.Errorf("session id = %q, want %q", moved.ID, sess.ID)
	}
	if moved.Round != 3 {
		t.Errorf("round = %d, want 3 — the position must survive", moved.Round)
	}
	if moved.Status != store.SessionRunning {
		t.Errorf("status = %q, want RUNNING", moved.Status)
	}
}

// The merge path must keep working exactly as before.
func TestPullRequestMergeStillRecords(t *testing.T) {
	withCommentSecret(t)
	srv, _ := setupWebhookTest(t)

	body := []byte(fmt.Sprintf(`{"action":"closed","pull_request":{"html_url":%q,"merged":true}}`,
		"https://github.com/acme/widgets/pull/99"))
	rec := postComment(t, srv, "pull_request", body)
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}
