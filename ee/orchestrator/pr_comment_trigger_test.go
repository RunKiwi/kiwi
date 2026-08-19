// SPDX-License-Identifier: LicenseRef-Kiwi-BSL-1.1
// Copyright (c) 2026 RunKiwi. Licensed under the Business Source License 1.1.
// See ee/LICENSE. This is Control Plane code and is NOT Apache-2.0.

package orchestrator

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/ibreakthecloud/kiwi/pkg/store"
)

// monitorTriggerAPI builds a fixture GitHub API server covering every call
// handleMonitorTrigger's path can make: resolving the PR's merge state
// (getPullRequest), checking a non-privileged commenter's permission
// (collaboratorPermission), and replying/reacting in the PR
// (createIssueComment, addReaction). commentsPosted counts how many replies
// were posted, which is how a rejection path (no monitor, but a reply) is
// told apart from a silent guard failure (no monitor, no reply either).
func monitorTriggerAPI(t *testing.T, merged bool, collaboratorWrite bool) (*httptest.Server, *int32) {
	t.Helper()
	var commentsPosted int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/pulls/42"):
			sha := strings.Repeat("e", 40)
			if merged {
				_, _ = w.Write([]byte(`{"merged":true,"merge_commit_sha":"` + sha + `"}`))
			} else {
				_, _ = w.Write([]byte(`{"merged":false}`))
			}
		case strings.Contains(r.URL.Path, "/collaborators/"):
			perm := "read"
			if collaboratorWrite {
				perm = "write"
			}
			_, _ = w.Write([]byte(`{"permission":"` + perm + `"}`))
		case strings.Contains(r.URL.Path, "/issues/42/comments"):
			atomic.AddInt32(&commentsPosted, 1)
			w.WriteHeader(http.StatusCreated)
		case strings.Contains(r.URL.Path, "/reactions"):
			w.WriteHeader(http.StatusCreated)
		default:
			t.Errorf("unexpected call to %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	return srv, &commentsPosted
}

// seedMonitorInstallation gives org1 a GitHub App installation on "acme",
// which is how handleCommentTrigger's monitor branch resolves orgID for a
// PR with no Kiwi task at all.
func seedMonitorInstallation(t *testing.T, s *store.PostgresStore, mode string) {
	t.Helper()
	if err := s.DB().Create(&store.Organization{ID: "org1", Name: "acme", PRCommentMode: mode}).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.DB().Create(&store.GitHubInstallation{
		InstallationID: testGitHubInstallationID, OrgID: "org1", AccountLogin: "acme",
	}).Error; err != nil {
		t.Fatal(err)
	}
}

// The happy path: "@runkiwi monitor this" on a merged PR with no Kiwi task
// creates an external monitor, resolving org via the GitHub installation
// rather than a task lookup.
func TestMonitorCommentTriggerCreatesAnExternalMonitor(t *testing.T) {
	withCommentSecret(t)
	srv, s := setupWebhookTest(t)
	seedMonitorInstallation(t, s, store.PRCommentModeMention)

	api, commentsPosted := monitorTriggerAPI(t, true, false)
	defer api.Close()
	withGithubAPIRedirect(t, api.URL)
	srv.githubApp = githubAppFixture(t).githubApp

	rec := postComment(t, srv, "issue_comment",
		commentDelivery("@runkiwi monitor this", "OWNER", "User", 901))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	monitors, err := s.ListMonitors(context.Background(), "org1")
	if err != nil {
		t.Fatal(err)
	}
	if len(monitors) != 1 {
		t.Fatalf("got %d monitors, want 1", len(monitors))
	}
	if monitors[0].Origin != store.MonitorOriginExternalPR {
		t.Errorf("origin = %q, want external_pr", monitors[0].Origin)
	}
	if monitors[0].Repo != "acme/widgets" || monitors[0].PRNumber != 42 {
		t.Errorf("repo/pr = %q/%d, want acme/widgets/42", monitors[0].Repo, monitors[0].PRNumber)
	}
	if got := atomic.LoadInt32(commentsPosted); got != 1 {
		t.Errorf("comments posted = %d, want 1 (the confirmation reply)", got)
	}
}

// GitHub redelivers the identical webhook (same CommentID) after the first
// delivery already created the monitor. createExternalMonitor's own dedupe
// on merge-commit SHA is what stops a second monitor from being created;
// handleMonitorTrigger does reply again on the redelivery ("a monitor
// already exists"), an accepted tradeoff documented on handleMonitorTrigger
// itself — the alternative is a schema change to track handled comment ids
// purely to silence a rare, low-stakes duplicate comment.
func TestMonitorCommentTriggerRedeliveryDoesNotDuplicateTheMonitor(t *testing.T) {
	withCommentSecret(t)
	srv, s := setupWebhookTest(t)
	seedMonitorInstallation(t, s, store.PRCommentModeMention)

	api, commentsPosted := monitorTriggerAPI(t, true, false)
	defer api.Close()
	withGithubAPIRedirect(t, api.URL)
	srv.githubApp = githubAppFixture(t).githubApp

	body := commentDelivery("@runkiwi monitor this", "OWNER", "User", 905)
	for i := 0; i < 2; i++ {
		if rec := postComment(t, srv, "issue_comment", body); rec.Code != http.StatusOK {
			t.Fatalf("delivery %d: status = %d, want 200", i+1, rec.Code)
		}
	}

	monitors, err := s.ListMonitors(context.Background(), "org1")
	if err != nil {
		t.Fatal(err)
	}
	if len(monitors) != 1 {
		t.Fatalf("got %d monitors after a redelivery, want 1 — no duplicate monitor", len(monitors))
	}
	// One confirmation reply from the first delivery, one "already exists"
	// reply from the second — see handleMonitorTrigger's doc comment for why
	// this duplicate reply is the accepted tradeoff rather than a bug.
	if got := atomic.LoadInt32(commentsPosted); got != 2 {
		t.Errorf("comments posted = %d, want 2 (confirmation + already-exists)", got)
	}
}

// A genuinely different comment (different CommentID — a different
// commenter, or the same one asking again later) on a PR that already has a
// monitor must get a reply saying so, exactly like the dashboard's POST
// /api/v1/monitors returns 409 for the same situation. This is the case
// commentAlreadyHandled could never distinguish from a true redelivery (it's
// keyed on queued_tasks, which this path never writes to), so it must not be
// silently swallowed the way a true redelivery's duplicate is tolerated.
func TestMonitorCommentTriggerOnAnAlreadyMonitoredPRGetsAConflictReply(t *testing.T) {
	withCommentSecret(t)
	srv, s := setupWebhookTest(t)
	seedMonitorInstallation(t, s, store.PRCommentModeMention)

	api, commentsPosted := monitorTriggerAPI(t, true, false)
	defer api.Close()
	withGithubAPIRedirect(t, api.URL)
	srv.githubApp = githubAppFixture(t).githubApp

	// First comment (id 906) creates the monitor.
	if rec := postComment(t, srv, "issue_comment",
		commentDelivery("@runkiwi monitor this", "OWNER", "User", 906)); rec.Code != http.StatusOK {
		t.Fatalf("first delivery: status = %d, want 200", rec.Code)
	}
	// A different comment (different id, different sender) asks again.
	if rec := postComment(t, srv, "issue_comment",
		commentDelivery("@runkiwi monitor this", "COLLABORATOR", "User", 907)); rec.Code != http.StatusOK {
		t.Fatalf("second delivery: status = %d, want 200", rec.Code)
	}

	monitors, err := s.ListMonitors(context.Background(), "org1")
	if err != nil {
		t.Fatal(err)
	}
	if len(monitors) != 1 {
		t.Fatalf("got %d monitors, want 1 — the second comment must not create another", len(monitors))
	}
	if got := atomic.LoadInt32(commentsPosted); got != 2 {
		t.Errorf("comments posted = %d, want 2 (confirmation + already-exists reply to the second comment)", got)
	}
}

// A monitor request on a PR that is not merged yet must be rejected with an
// explanatory reply, and must create nothing.
func TestMonitorCommentTriggerOnAnOpenPRIsRejectedWithAReply(t *testing.T) {
	withCommentSecret(t)
	srv, s := setupWebhookTest(t)
	seedMonitorInstallation(t, s, store.PRCommentModeMention)

	api, commentsPosted := monitorTriggerAPI(t, false, false)
	defer api.Close()
	withGithubAPIRedirect(t, api.URL)
	srv.githubApp = githubAppFixture(t).githubApp

	rec := postComment(t, srv, "issue_comment",
		commentDelivery("@runkiwi monitor this", "OWNER", "User", 902))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	monitors, err := s.ListMonitors(context.Background(), "org1")
	if err != nil {
		t.Fatal(err)
	}
	if len(monitors) != 0 {
		t.Fatalf("got %d monitors, want 0 for an unmerged PR", len(monitors))
	}
	if got := atomic.LoadInt32(commentsPosted); got != 1 {
		t.Errorf("comments posted = %d, want 1 (the rejection reply)", got)
	}
}

// A commenter without write access must not be able to create a monitor,
// exactly like they cannot trigger a continuation today.
func TestMonitorCommentTriggerRespectsWriteAccessGuard(t *testing.T) {
	withCommentSecret(t)
	srv, s := setupWebhookTest(t)
	seedMonitorInstallation(t, s, store.PRCommentModeMention)

	api, commentsPosted := monitorTriggerAPI(t, true, false)
	defer api.Close()
	withGithubAPIRedirect(t, api.URL)
	srv.githubApp = githubAppFixture(t).githubApp

	rec := postComment(t, srv, "issue_comment",
		commentDelivery("@runkiwi monitor this", "CONTRIBUTOR", "User", 903))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	monitors, err := s.ListMonitors(context.Background(), "org1")
	if err != nil {
		t.Fatal(err)
	}
	if len(monitors) != 0 {
		t.Fatalf("got %d monitors, want 0 — the commenter has no write access", len(monitors))
	}
	if got := atomic.LoadInt32(commentsPosted); got != 0 {
		t.Errorf("comments posted = %d, want 0 — a refused instruction gets no reply", got)
	}
}

// An org that disabled comment-driven behavior entirely gets no
// comment-driven monitor creation either, even for a valid "@runkiwi
// monitor this" on a merged PR from a privileged commenter.
func TestMonitorCommentTriggerRespectsPRCommentModeOff(t *testing.T) {
	withCommentSecret(t)
	srv, s := setupWebhookTest(t)
	seedMonitorInstallation(t, s, store.PRCommentModeOff)

	api, commentsPosted := monitorTriggerAPI(t, true, false)
	defer api.Close()
	withGithubAPIRedirect(t, api.URL)
	srv.githubApp = githubAppFixture(t).githubApp

	rec := postComment(t, srv, "issue_comment",
		commentDelivery("@runkiwi monitor this", "OWNER", "User", 904))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	monitors, err := s.ListMonitors(context.Background(), "org1")
	if err != nil {
		t.Fatal(err)
	}
	if len(monitors) != 0 {
		t.Fatalf("got %d monitors, want 0 — PRCommentMode is off", len(monitors))
	}
	if got := atomic.LoadInt32(commentsPosted); got != 0 {
		t.Errorf("comments posted = %d, want 0 — mode off means no reply either", got)
	}
}
