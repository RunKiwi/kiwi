// SPDX-License-Identifier: LicenseRef-Kiwi-BSL-1.1
// Copyright (c) 2026 RunKiwi. Licensed under the Business Source License 1.1.
// See ee/LICENSE. This is Control Plane code and is NOT Apache-2.0.

package orchestrator

import (
	"encoding/json"
	"regexp"
	"strings"
)

// Turning a review comment into an instruction.
//
// Everything in this file is a pure function over a webhook payload, on
// purpose. These are the rules most likely to be subtly wrong — who may spend
// the org's money, what counts as being addressed, which events even refer to
// a pull request — and each mistake is expensive in a different way: one bills
// a customer for a compliment, one lets a stranger drive the budget, and one
// starts an infinite loop of Kiwi replying to itself.
//
// Keeping them free of the database and the network is what makes it cheap to
// enumerate every case in a table test.

// commentTrigger is a comment that might be an instruction, with everything
// the guards need to decide.
type commentTrigger struct {
	// PRURL is the pull request's html_url, which is what queued_tasks stores
	// as result_url and therefore how the task is found.
	PRURL       string
	PRNumber    int
	PROpen      bool
	Owner, Repo string

	Body      string
	CommentID int64

	SenderLogin string
	SenderIsBot bool
	// Association is GitHub's author_association: OWNER, MEMBER, COLLABORATOR,
	// CONTRIBUTOR, NONE, and friends.
	Association string
}

// The wire shapes. One struct covers all three events: the fields that matter
// appear in each under a different key, and absence is how they are told apart.
type commentPayload struct {
	Action string `json:"action"`
	Issue  struct {
		Number  int    `json:"number"`
		State   string `json:"state"`
		HTMLURL string `json:"html_url"`
		// PullRequest is present only when the issue IS a pull request. An
		// issue_comment fires for plain issues too, and those have no PR to
		// continue.
		PullRequest *struct {
			URL string `json:"url"`
		} `json:"pull_request"`
	} `json:"issue"`
	PullRequest struct {
		Number  int    `json:"number"`
		State   string `json:"state"`
		Merged  bool   `json:"merged"`
		HTMLURL string `json:"html_url"`
	} `json:"pull_request"`
	Comment struct {
		ID                int64  `json:"id"`
		Body              string `json:"body"`
		AuthorAssociation string `json:"author_association"`
	} `json:"comment"`
	Review struct {
		ID                int64  `json:"id"`
		Body              string `json:"body"`
		AuthorAssociation string `json:"author_association"`
	} `json:"review"`
	Repository struct {
		Name  string `json:"name"`
		Owner struct {
			Login string `json:"login"`
		} `json:"owner"`
	} `json:"repository"`
	Sender struct {
		Login string `json:"login"`
		Type  string `json:"type"`
	} `json:"sender"`
}

// parseCommentEvent extracts a trigger from a webhook delivery, reporting
// false when the event cannot possibly be one.
//
// Note it returns a trigger for a bot sender rather than rejecting it here:
// the caller checks that, so the reason a delivery was ignored stays visible
// in one ordered list of guards instead of being split between two files.
func parseCommentEvent(event string, body []byte) (*commentTrigger, bool) {
	var p commentPayload
	if err := json.Unmarshal(body, &p); err != nil {
		return nil, false
	}

	t := &commentTrigger{
		Owner:       p.Repository.Owner.Login,
		Repo:        p.Repository.Name,
		SenderLogin: p.Sender.Login,
		SenderIsBot: strings.EqualFold(p.Sender.Type, "Bot") ||
			strings.HasSuffix(p.Sender.Login, "[bot]"),
	}

	switch event {
	case "issue_comment":
		// An issue that is not a pull request has nothing to continue.
		if p.Issue.PullRequest == nil {
			return nil, false
		}
		t.PRURL = p.Issue.HTMLURL
		t.PRNumber = p.Issue.Number
		t.PROpen = p.Issue.State == "open"
		t.Body = p.Comment.Body
		t.CommentID = p.Comment.ID
		t.Association = p.Comment.AuthorAssociation

	case "pull_request_review_comment":
		t.PRURL = p.PullRequest.HTMLURL
		t.PRNumber = p.PullRequest.Number
		t.PROpen = p.PullRequest.State == "open" && !p.PullRequest.Merged
		t.Body = p.Comment.Body
		t.CommentID = p.Comment.ID
		t.Association = p.Comment.AuthorAssociation

	case "pull_request_review":
		t.PRURL = p.PullRequest.HTMLURL
		t.PRNumber = p.PullRequest.Number
		t.PROpen = p.PullRequest.State == "open" && !p.PullRequest.Merged
		t.Body = p.Review.Body
		t.CommentID = p.Review.ID
		t.Association = p.Review.AuthorAssociation

	default:
		return nil, false
	}

	if t.PRURL == "" || t.CommentID == 0 {
		return nil, false
	}
	return t, true
}

// mentionRe matches an @-mention of Kiwi and nothing else. The leading
// boundary is what stops "email runkiwi@example.com" and a bare "runkiwi"
// counting, and the trailing one stops "@runkiwithing".
var mentionRe = regexp.MustCompile(`(?i)(^|[^\w@/])@runkiwi(-bot)?\b`)

// mentionsKiwi reports whether a comment addresses Kiwi directly.
func mentionsKiwi(body string) bool {
	return mentionRe.MatchString(body)
}

// instructionFrom returns the comment with the mention removed, which is what
// the Architect is asked to act on. "@runkiwi rename the variable" is an
// instruction to rename a variable; leaving the handle in invites the model to
// treat its own name as part of the task.
func instructionFrom(body string) string {
	stripped := mentionRe.ReplaceAllString(body, "$1")
	return strings.TrimSpace(strings.Join(strings.Fields(stripped), " "))
}

// associationGrantsWrite reports whether GitHub's author_association alone
// proves write access, saving an API call for the common cases.
//
// CONTRIBUTOR is deliberately absent: it means somebody whose pull request was
// merged once, not somebody who can push. On a public repository that is the
// difference between a teammate and a stranger with an opinion.
func associationGrantsWrite(assoc string) bool {
	switch strings.ToUpper(assoc) {
	case "OWNER", "MEMBER", "COLLABORATOR":
		return true
	}
	return false
}

// permissionGrantsWrite reads the answer from the collaborators/permission
// endpoint. "triage" can label and close but not push, so it does not qualify.
func permissionGrantsWrite(permission string) bool {
	switch strings.ToLower(permission) {
	case "admin", "maintain", "write":
		return true
	}
	return false
}
