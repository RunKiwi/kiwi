// SPDX-License-Identifier: LicenseRef-Kiwi-BSL-1.1
// Copyright (c) 2026 RunKiwi. Licensed under the Business Source License 1.1.
// See ee/LICENSE. This is Control Plane code and is NOT Apache-2.0.

package orchestrator

import "testing"

const issueCommentOnPR = `{
  "action": "created",
  "issue": {
    "number": 42,
    "state": "open",
    "html_url": "https://github.com/acme/widgets/pull/42",
    "pull_request": {"url": "https://api.github.com/repos/acme/widgets/pulls/42"}
  },
  "comment": {
    "id": 5551212,
    "body": "@runkiwi rename the variable",
    "author_association": "COLLABORATOR",
    "user": {"login": "reviewer", "type": "User"}
  },
  "repository": {"name": "widgets", "owner": {"login": "acme"}},
  "sender": {"login": "reviewer", "type": "User"}
}`

// An issue_comment fires for plain issues too, and those have no pull_request
// key. Acting on one would look up a PR URL that is really an issue URL.
const issueCommentOnIssue = `{
  "action": "created",
  "issue": {"number": 7, "state": "open", "html_url": "https://github.com/acme/widgets/issues/7"},
  "comment": {"id": 99, "body": "@runkiwi fix it", "author_association": "OWNER", "user": {"login": "u", "type": "User"}},
  "repository": {"name": "widgets", "owner": {"login": "acme"}},
  "sender": {"login": "u", "type": "User"}
}`

const reviewComment = `{
  "action": "created",
  "pull_request": {"number": 42, "state": "open", "merged": false, "html_url": "https://github.com/acme/widgets/pull/42"},
  "comment": {"id": 777, "body": "@runkiwi this loop is wrong", "author_association": "MEMBER", "user": {"login": "r", "type": "User"}},
  "repository": {"name": "widgets", "owner": {"login": "acme"}},
  "sender": {"login": "r", "type": "User"}
}`

const reviewSubmitted = `{
  "action": "submitted",
  "pull_request": {"number": 42, "state": "open", "merged": false, "html_url": "https://github.com/acme/widgets/pull/42"},
  "review": {"id": 888, "body": "@runkiwi please split this", "author_association": "OWNER", "state": "changes_requested", "user": {"login": "o", "type": "User"}},
  "repository": {"name": "widgets", "owner": {"login": "acme"}},
  "sender": {"login": "o", "type": "User"}
}`

const botComment = `{
  "action": "created",
  "issue": {"number": 42, "state": "open", "html_url": "https://github.com/acme/widgets/pull/42",
            "pull_request": {"url": "https://api.github.com/repos/acme/widgets/pulls/42"}},
  "comment": {"id": 4242, "body": "@runkiwi pushed round 3", "author_association": "NONE", "user": {"login": "runkiwi[bot]", "type": "Bot"}},
  "repository": {"name": "widgets", "owner": {"login": "acme"}},
  "sender": {"login": "runkiwi[bot]", "type": "Bot"}
}`

func TestParseCommentEvent_IssueCommentOnAPullRequest(t *testing.T) {
	got, ok := parseCommentEvent("issue_comment", []byte(issueCommentOnPR))
	if !ok {
		t.Fatal("expected a trigger")
	}
	if got.PRURL != "https://github.com/acme/widgets/pull/42" {
		t.Errorf("pr url = %q", got.PRURL)
	}
	if got.CommentID != 5551212 {
		t.Errorf("comment id = %d, want 5551212", got.CommentID)
	}
	if got.Owner != "acme" || got.Repo != "widgets" {
		t.Errorf("repo = %s/%s, want acme/widgets", got.Owner, got.Repo)
	}
	if got.PRNumber != 42 {
		t.Errorf("pr number = %d, want 42", got.PRNumber)
	}
	if got.Association != "COLLABORATOR" {
		t.Errorf("association = %q", got.Association)
	}
	if got.SenderIsBot {
		t.Error("a User sender must not read as a bot")
	}
	if !got.PROpen {
		t.Error("an open PR must read as open")
	}
}

// The one that would otherwise send a task to an issue URL that no PR lookup
// can match.
func TestParseCommentEvent_PlainIssueIsNotATrigger(t *testing.T) {
	if _, ok := parseCommentEvent("issue_comment", []byte(issueCommentOnIssue)); ok {
		t.Error("an issue comment on a non-PR issue must not be a trigger")
	}
}

func TestParseCommentEvent_ReviewCommentAndReviewBody(t *testing.T) {
	for _, tc := range []struct {
		event string
		body  string
		id    int64
	}{
		{"pull_request_review_comment", reviewComment, 777},
		{"pull_request_review", reviewSubmitted, 888},
	} {
		got, ok := parseCommentEvent(tc.event, []byte(tc.body))
		if !ok {
			t.Fatalf("%s: expected a trigger", tc.event)
		}
		if got.CommentID != tc.id {
			t.Errorf("%s: comment id = %d, want %d", tc.event, got.CommentID, tc.id)
		}
		if got.PRURL != "https://github.com/acme/widgets/pull/42" {
			t.Errorf("%s: pr url = %q", tc.event, got.PRURL)
		}
	}
}

// Kiwi's own reply mentions Kiwi. Without this the first reply starts a round
// whose reply starts a round, billed to the customer until something breaks.
func TestParseCommentEvent_BotSenderIsMarked(t *testing.T) {
	got, ok := parseCommentEvent("issue_comment", []byte(botComment))
	if !ok {
		t.Fatal("expected a trigger to be parsed, with the bot flag set")
	}
	if !got.SenderIsBot {
		t.Error("a Bot sender must read as a bot")
	}
}

func TestParseCommentEvent_UnknownEventIsIgnored(t *testing.T) {
	if _, ok := parseCommentEvent("push", []byte(`{}`)); ok {
		t.Error("an unrelated event must not be a trigger")
	}
}

func TestMentionsKiwi(t *testing.T) {
	for _, body := range []string{
		"@runkiwi rename the variable",
		"please @RunKiwi have another look",
		"@runkiwi-bot do it",
		"line one\n@runkiwi line two",
	} {
		if !mentionsKiwi(body) {
			t.Errorf("missed a mention in %q", body)
		}
	}
	for _, body := range []string{
		"runkiwi should look at this",
		"nice work",
		"email runkiwi@example.com",
		"",
	} {
		if mentionsKiwi(body) {
			t.Errorf("found a mention in %q, want none", body)
		}
	}
}

func TestInstructionFromStripsTheMention(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"@runkiwi rename the variable", "rename the variable"},
		{"  @RunKiwi   rename it  ", "rename it"},
		{"please @runkiwi-bot split this file", "please split this file"},
		{"@runkiwi", ""},
	} {
		if got := instructionFrom(tc.in); got != tc.want {
			t.Errorf("instructionFrom(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// Write access is what spends the org's agent-minutes. On a public repo this
// is the only thing between a stranger and the budget.
func TestAssociationGrantsWrite(t *testing.T) {
	for _, assoc := range []string{"OWNER", "MEMBER", "COLLABORATOR"} {
		if !associationGrantsWrite(assoc) {
			t.Errorf("%s should grant write", assoc)
		}
	}
	for _, assoc := range []string{"CONTRIBUTOR", "FIRST_TIME_CONTRIBUTOR", "FIRST_TIMER", "NONE", "MANNEQUIN", ""} {
		if associationGrantsWrite(assoc) {
			t.Errorf("%s must not grant write", assoc)
		}
	}
}

func TestPermissionGrantsWrite(t *testing.T) {
	for _, p := range []string{"admin", "maintain", "write"} {
		if !permissionGrantsWrite(p) {
			t.Errorf("%s should grant write", p)
		}
	}
	for _, p := range []string{"triage", "read", "none", ""} {
		if permissionGrantsWrite(p) {
			t.Errorf("%s must not grant write", p)
		}
	}
}
