// ee/orchestrator/slack_completion.go
// SPDX-License-Identifier: LicenseRef-Kiwi-BSL-1.1
// Copyright (c) 2026 RunKiwi. Licensed under the Business Source License 1.1.
// See ee/LICENSE. This is Control Plane code and is NOT Apache-2.0.

package orchestrator

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/ibreakthecloud/kiwi/pkg/gitcache"
	"github.com/ibreakthecloud/kiwi/pkg/store"
)

// reportSlackCompletion edits a Slack-triggered task's status message to its
// terminal state. Called from handleDaemonResult for EVERY task regardless
// of origin — it is a no-op (not an error) for a task that didn't come from
// Slack, which is the common case and must stay cheap and silent.
func (s *Server) reportSlackCompletion(ctx context.Context, taskID string, task *store.QueuedTask) {
	row, err := s.storage.GetSlackTriggeredTaskByQueuedTaskID(ctx, taskID)
	if err != nil || row == nil {
		return
	}
	if s.slackClient == nil {
		return
	}

	token, err := s.storage.GetCredentialPlaintext(ctx, row.OrgID, "SLACK_BOT_TOKEN")
	if err != nil || token == "" {
		return
	}

	// ResultURL and ResultDetail are *string on store.QueuedTask (nil means
	// "never set", same convention jobs_api.go and ver_hook.go already read
	// them with) — resolve both to plain strings once, up front.
	var resultURL, resultDetail string
	if task.ResultURL != nil {
		resultURL = *task.ResultURL
	}
	if task.ResultDetail != nil {
		resultDetail = *task.ResultDetail
	}

	var text, status string
	switch {
	case task.Status == store.TaskSucceeded && resultURL != "":
		text = fmt.Sprintf(":white_check_mark: Done — %s", resultURL)
		status = "succeeded"
	case task.Status == store.TaskSucceeded: // investigation-only completion (Task 12): no PR, findings in ResultDetail
		text = fmt.Sprintf(":white_check_mark: %s", truncateForSlack(resultDetail))
		status = "succeeded"
	default:
		text = fmt.Sprintf(":x: %s", truncateForSlack(resultDetail))
		status = "failed"
	}

	if task.Status == store.TaskSucceeded && resultURL == "" && wantsIssueCreation(taskInstruction(task)) {
		if ghToken, ok := s.installationToken(ctx, row.OrgID); ok {
			owner, repo, ok := ownerRepoFromSpec(task.Spec)
			if ok {
				if url, err := createIssue(ctx, githubAPIDefault, ghToken, owner, repo, issueTitle(task), resultDetail); err == nil {
					text += fmt.Sprintf("\nFiled as %s", url)
				} else {
					log.Printf("[slackapp] creating issue for task %s: %v", taskID, err)
				}
			}
		}
	}

	if row.StatusMessageTS != "" {
		if err := s.slackClient.EditMessage(ctx, token, row.ChannelID, row.StatusMessageTS, text); err != nil {
			log.Printf("[slackapp] editing status message for task %s: %v", taskID, err)
		}
	}
	if err := s.storage.UpdateSlackTriggeredTaskStatus(ctx, row.ID, status, ""); err != nil {
		log.Printf("[slackapp] updating status row for task %s: %v", taskID, err)
	}
}

// wantsIssueCreation reports whether the instruction explicitly asked for a
// GitHub issue — a bounded, opt-in action, not a default behavior for every
// investigation-only completion.
func wantsIssueCreation(instruction string) bool {
	lower := strings.ToLower(instruction)
	return strings.Contains(lower, "create a github issue") || strings.Contains(lower, "create an issue") || strings.Contains(lower, "open an issue") || strings.Contains(lower, "file an issue")
}

// taskInstruction is the original objective this task was given, stored on
// every QueuedTask's spec under "task" (see PlannedWorker.Task /
// SubmitPlan's spec map). Empty when the field is somehow missing rather
// than panicking — wantsIssueCreation("") is simply false.
func taskInstruction(task *store.QueuedTask) string {
	if task.Spec == nil {
		return ""
	}
	s, _ := task.Spec["task"].(string)
	return s
}

// ownerRepoFromSpec resolves the task's repo_url back to an owner/repo pair
// for the GitHub issues API, which addresses by owner/repo rather than by
// URL.
func ownerRepoFromSpec(spec map[string]interface{}) (owner, repo string, ok bool) {
	if spec == nil {
		return "", "", false
	}
	url, _ := spec["repo_url"].(string)
	r, ok := gitcache.ParseRepo(url)
	if !ok {
		return "", "", false
	}
	return r.Owner, r.Name, true
}

// issueTitle derives a short issue title from the task's own instruction —
// its first line, since an instruction can run to several sentences but a
// GitHub issue title is meant to be a one-line summary.
func issueTitle(task *store.QueuedTask) string {
	instruction := taskInstruction(task)
	if i := strings.IndexByte(instruction, '\n'); i != -1 {
		instruction = instruction[:i]
	}
	if len(instruction) > 120 {
		instruction = instruction[:120] + "…"
	}
	if instruction == "" {
		return "Kiwi investigation"
	}
	return instruction
}

// truncateForSlack keeps a long investigation report or failure detail from
// blowing past Slack's per-message size limit, pointing at the dashboard
// task page for the rest — the spec's "view full report" behavior.
func truncateForSlack(detail string) string {
	const max = 2000
	if len(detail) <= max {
		return detail
	}
	return detail[:max] + "… (truncated — see the full report on the Kiwi dashboard)"
}
