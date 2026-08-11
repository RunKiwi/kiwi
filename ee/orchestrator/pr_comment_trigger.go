// SPDX-License-Identifier: LicenseRef-Kiwi-BSL-1.1
// Copyright (c) 2026 RunKiwi. Licensed under the Business Source License 1.1.
// See ee/LICENSE. This is Control Plane code and is NOT Apache-2.0.

package orchestrator

import (
	"context"
	"errors"
	"log"
	"net/http"

	"github.com/ibreakthecloud/kiwi/ee/githubapp"
	"github.com/ibreakthecloud/kiwi/ee/planner"
	"github.com/ibreakthecloud/kiwi/pkg/store"
)

// Deciding whether a review comment becomes the next round.
//
// Every path returns without an error: the caller answers 200 regardless,
// because a non-2xx teaches GitHub to disable the hook — which would take the
// merge records down with the comments. A refusal is therefore a log line, and
// sometimes a reply in the pull request, never a failed delivery.
//
// The guards run cheapest-first, with one deviation from the design: the org's
// mode cannot be read before the task is found, because the mode is per-org
// and the org is only known once the pull request resolves to a task. The
// dedupe check moved ahead of the permission call for the same kind of reason
// — it is a local read, and the permission check may cost a network round trip.
func (s *Server) handleCommentTrigger(r *http.Request, event string, t *commentTrigger) {
	ctx := r.Context()

	// 1. Kiwi's own reply mentions Kiwi. Without this the first reply starts a
	// round whose reply starts a round, billed to the customer until something
	// breaks.
	if t.SenderIsBot {
		return
	}

	// 2. Which task opened this pull request. No match means it is not ours.
	parent, err := s.taskForPR(ctx, t.PRURL)
	if err != nil || parent == nil {
		return
	}

	// 3. What this org has asked for.
	mode, err := s.storage.PRCommentMode(ctx, parent.OrgID)
	if err != nil {
		log.Printf("[pr-comment] reading the mode for org %s: %v", parent.OrgID, err)
		return
	}
	switch mode {
	case store.PRCommentModeOff:
		return
	case store.PRCommentModeMention:
		if !mentionsKiwi(t.Body) {
			return
		}
	}

	// 4. Something to act on. "@runkiwi" alone is an acknowledgement, not an
	// instruction, and handing an empty objective to the Architect spends a
	// round to be told there is nothing to do.
	instruction := instructionFrom(t.Body)
	if instruction == "" {
		return
	}

	// 5. A merged or closed pull request's branch is spent. Continuing onto it
	// would push work nobody is reviewing.
	if !t.PROpen {
		return
	}

	// 6. GitHub redelivers. The unique index on trigger_comment_id is the real
	// guarantee; this check is what keeps the common case quiet rather than
	// relying on a constraint violation.
	if seen, err := s.commentAlreadyHandled(ctx, parent.OrgID, t.CommentID); err != nil || seen {
		return
	}

	// 7. Write access is what spends the org's agent-minutes. On a public
	// repository this is the only thing between a stranger and the budget.
	if !s.commenterMayInstruct(ctx, parent.OrgID, t) {
		log.Printf("[pr-comment] %s has no write access to %s/%s; ignoring", t.SenderLogin, t.Owner, t.Repo)
		return
	}

	// 8. One continuation at a time. Two tasks in a thread share a branch and
	// both force-push to it, so the loser's work would vanish silently. Say so
	// rather than dropping the instruction: the reviewer would otherwise
	// believe Kiwi is working on it.
	root := parent.RootTaskID
	if root == "" {
		root = parent.ID
	}
	if active, err := s.storage.ActiveTaskInThread(ctx, parent.OrgID, root); err == nil && active != nil {
		s.replyInPR(ctx, parent.OrgID, t,
			"Still working on the previous round — comment again once this one lands and I'll pick it up.")
		return
	}

	// The session, when there is one. A parent that ran the single-file loop
	// has none, and a continuation of it is simply a fresh run on the same
	// branch rather than a resumed conversation.
	sessionID := ""
	if sess, serr := s.storage.GetAgentSessionByTask(ctx, parent.OrgID, parent.ID); serr == nil && sess != nil {
		sessionID = sess.ID
	}

	task, err := s.planner.SubmitContinuation(ctx, planner.ContinuationInput{
		OrgID:       parent.OrgID,
		ParentTask:  parent,
		Instruction: instruction,
		CommentID:   t.CommentID,
		SessionID:   sessionID,
	})
	if err != nil {
		log.Printf("[pr-comment] continuing task %s from comment %d: %v", parent.ID, t.CommentID, err)
		return
	}
	log.Printf("[pr-comment] comment %d continues task %s as %s", t.CommentID, parent.ID, task.ID)

	// Acknowledge last, once the work is really queued: an eye on a comment
	// that produced nothing is worse than no eye at all.
	s.acknowledge(ctx, parent.OrgID, event, t)
}

// taskForPR resolves a pull request URL to the task that opened it, using the
// same result_url lookup the merge path has always used.
func (s *Server) taskForPR(ctx context.Context, prURL string) (*store.QueuedTask, error) {
	var task store.QueuedTask
	if err := s.storage.DB().WithContext(ctx).
		Where("result_url = ?", prURL).
		Order("created_at desc").
		First(&task).Error; err != nil {
		return nil, err
	}
	return &task, nil
}

// commentAlreadyHandled reports whether this comment has already produced a
// task.
func (s *Server) commentAlreadyHandled(ctx context.Context, orgID string, commentID int64) (bool, error) {
	var count int64
	err := s.storage.DB().WithContext(ctx).Model(&store.QueuedTask{}).
		Where("org_id = ? AND trigger_comment_id = ?", orgID, commentID).
		Count(&count).Error
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// commenterMayInstruct decides whether this person may spend the org's money.
//
// author_association settles the common cases for free. Anything else costs a
// call, and a call that cannot be made is a refusal: without an installation
// token there is no way to establish write access, and guessing in the
// permissive direction on a public repository hands the budget to whoever
// comments first.
func (s *Server) commenterMayInstruct(ctx context.Context, orgID string, t *commentTrigger) bool {
	if associationGrantsWrite(t.Association) {
		return true
	}

	token, ok := s.installationToken(ctx, orgID)
	if !ok {
		return false
	}
	perm, err := collaboratorPermission(ctx, githubAPIDefault, token, t.Owner, t.Repo, t.SenderLogin)
	if err != nil {
		log.Printf("[pr-comment] checking %s's permission on %s/%s: %v", t.SenderLogin, t.Owner, t.Repo, err)
		return false
	}
	return permissionGrantsWrite(perm)
}

// installationToken mints a token for any installation this org has. A missing
// App, no installation, or a revoked one all mean the same thing to a caller:
// there is no token, so whatever needed it does not happen.
func (s *Server) installationToken(ctx context.Context, orgID string) (string, bool) {
	if s.githubApp == nil {
		return "", false
	}
	installs, err := s.storage.ListGitHubInstallations(ctx, orgID)
	if err != nil || len(installs) == 0 {
		return "", false
	}
	for _, inst := range installs {
		tok, err := s.githubApp.InstallationToken(ctx, inst.InstallationID)
		if err != nil {
			if errors.Is(err, githubapp.ErrInstallationGone) {
				if delErr := s.storage.DeleteGitHubInstallation(ctx, inst.InstallationID); delErr != nil {
					log.Printf("[githubapp] clearing revoked installation %d: %v", inst.InstallationID, delErr)
				}
			}
			continue
		}
		return tok.Value, true
	}
	return "", false
}

// acknowledge puts an eye on the comment so the reviewer knows within seconds
// rather than whenever the round finishes. Best effort: the work is already
// queued, and failing to react is not a reason to fail anything else.
func (s *Server) acknowledge(ctx context.Context, orgID, event string, t *commentTrigger) {
	token, ok := s.installationToken(ctx, orgID)
	if !ok {
		return
	}
	if err := addReaction(ctx, githubAPIDefault, token, t.Owner, t.Repo, event, t.CommentID, "eyes"); err != nil {
		log.Printf("[pr-comment] acknowledging comment %d: %v", t.CommentID, err)
	}
}

// replyInPR says something in the pull request's conversation. Also best
// effort, and also silent on failure: this is how Kiwi explains a refusal, not
// how it does the work.
func (s *Server) replyInPR(ctx context.Context, orgID string, t *commentTrigger, body string) {
	token, ok := s.installationToken(ctx, orgID)
	if !ok {
		return
	}
	if err := createIssueComment(ctx, githubAPIDefault, token, t.Owner, t.Repo, t.PRNumber, body); err != nil {
		log.Printf("[pr-comment] replying on %s/%s#%d: %v", t.Owner, t.Repo, t.PRNumber, err)
	}
}
