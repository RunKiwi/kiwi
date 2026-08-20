// ee/orchestrator/slack_trigger.go
// SPDX-License-Identifier: LicenseRef-Kiwi-BSL-1.1
// Copyright (c) 2026 RunKiwi. Licensed under the Business Source License 1.1.
// See ee/LICENSE. This is Control Plane code and is NOT Apache-2.0.

package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"log"
	"regexp"
	"strings"

	"github.com/ibreakthecloud/kiwi/ee/planner"
	"github.com/ibreakthecloud/kiwi/pkg/store"
)

var slackMentionRe = regexp.MustCompile(`<@[A-Z0-9]+>`)

func instructionFromSlack(text string) string {
	stripped := slackMentionRe.ReplaceAllString(text, "")
	return strings.TrimSpace(strings.Join(strings.Fields(stripped), " "))
}

// handleSlackTrigger turns an @mention into a task. Every path returns
// without an error and answers nothing back to the caller beyond a log line
// — the webhook handler has already answered Slack, matching
// handleCommentTrigger's "a refusal is a log line, never a failed
// delivery" posture.
//
// Scoped narrowly for now: repo resolution only checks a channel binding.
// Task 9 layers inline-override and LLM inference in ahead of this. Context
// is only the trigger message itself; Task 8 layers thread/channel history
// assembly in before this reaches the planner.
func (s *Server) handleSlackTrigger(ctx context.Context, teamID, channelID, threadTS, userID, text string) {
	inst, err := s.storage.GetSlackInstallationByTeamID(ctx, teamID)
	if err != nil || inst == nil {
		return // unknown team: nothing this delivery can do
	}

	token, err := s.storage.GetCredentialPlaintext(ctx, inst.OrgID, "SLACK_BOT_TOKEN")
	if err != nil || token == "" {
		log.Printf("[slackapp] org %s has an installation but no bot token", inst.OrgID)
		return
	}

	// A trigger with no thread yet starts its own: the trigger message's own
	// ts is the thread root every reply (and every status edit) anchors to.
	if threadTS == "" {
		threadTS = "" // set below once we know the status message's ts
	}

	instruction := instructionFromSlack(text)
	if instruction == "" {
		return
	}

	binding, err := s.storage.GetSlackChannelBinding(ctx, teamID, channelID)
	if err != nil || binding == nil {
		if s.slackClient != nil {
			s.slackClient.PostMessage(ctx, token, channelID, threadTS,
				"This channel isn't bound to a repository yet — an admin can set one up under Integrations.")
		}
		return
	}

	if s.slackClient != nil {
		s.slackClient.AddReaction(ctx, token, channelID, firstNonEmpty(threadTS, ""), "eyes")
	}

	result, err := s.planner.SubmitPlan(ctx, planner.PlanRequest{
		OrgID:   inst.OrgID,
		UserID:  userID,
		Task:    instruction,
		RepoURL: binding.RepoURL,
		Ref:     binding.DefaultRef,
		TestCmd: binding.DefaultTestCmd, // empty is fine: pkg/daemon infers it (see infer.go)
	})
	if err != nil {
		if s.slackClient != nil {
			s.slackClient.PostMessage(ctx, token, channelID, threadTS, fmt.Sprintf("Couldn't start that task: %s", err.Error()))
		}
		return
	}

	rootTS := threadTS
	statusTS := ""
	if s.slackClient != nil {
		statusTS, err = s.slackClient.PostMessage(ctx, token, channelID, threadTS,
			fmt.Sprintf("Working on it — job `%s`.", result.JobID))
		if err != nil {
			log.Printf("[slackapp] posting status message for job %s: %v", result.JobID, err)
		}
		if rootTS == "" {
			rootTS = statusTS // a fresh top-level trigger starts its own thread at its own status reply
		}
	}

	row := &store.SlackTriggeredTask{
		OrgID: inst.OrgID, TeamID: teamID, ChannelID: channelID, ThreadTS: rootTS,
		QueuedTaskID: firstOf(result.TaskIDs), StatusMessageTS: statusTS, LastStatus: "running",
	}
	if err := s.storage.CreateSlackTriggeredTask(ctx, row); err != nil {
		log.Printf("[slackapp] persist triggered-task row for job %s: %v", result.JobID, err)
	}
}

func firstOf(ids []string) string {
	if len(ids) == 0 {
		return ""
	}
	return ids[0]
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

var errNotImplemented = errors.New("not implemented")

// handleSlackInteractivity is a placeholder wired up for real in Task 11
// (continue/fork/new button clicks). Left as a named no-op rather than
// omitted so slack_webhook.go's dispatch has something to call today.
func (s *Server) handleSlackInteractivity(ctx context.Context, formBody []byte) {
	_ = ctx
	_ = formBody
}
