// ee/orchestrator/slack_trigger.go
// SPDX-License-Identifier: LicenseRef-Kiwi-BSL-1.1
// Copyright (c) 2026 RunKiwi. Licensed under the Business Source License 1.1.
// See ee/LICENSE. This is Control Plane code and is NOT Apache-2.0.

package orchestrator

import (
	"context"
	"encoding/json"
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

	if threadTS != "" {
		if existing, err := s.storage.LatestSlackTriggeredTask(ctx, teamID, channelID, threadTS); err == nil && existing != nil {
			s.handleSlackThreadReply(ctx, teamID, channelID, threadTS, userID, text, existing)
			return
		}
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

	rawInstruction := instructionFromSlack(text)
	if rawInstruction == "" {
		return
	}
	instruction, err := s.fetchSlackContext(ctx, token, channelID, threadTS, rawInstruction)
	if err != nil {
		instruction = rawInstruction
	}

	binding, _ := s.storage.GetSlackChannelBinding(ctx, teamID, channelID)
	// A binding survives a workspace being re-installed under a different
	// org — team_id isn't a stable proxy for "this org still owns it" once
	// that happens. A stale binding must not be trusted for a repo it was
	// never re-confirmed against under the org that owns the workspace now.
	if binding != nil && binding.OrgID != inst.OrgID {
		binding = nil
	}
	repoURL, ambiguousReply := s.resolveSlackRepo(ctx, inst.OrgID, text, binding)
	if ambiguousReply != "" {
		if s.slackClient != nil {
			s.slackClient.PostMessage(ctx, token, channelID, threadTS, ambiguousReply)
		}
		return
	}

	if s.slackClient != nil {
		s.slackClient.AddReaction(ctx, token, channelID, firstNonEmpty(threadTS, ""), "eyes")
	}

	var defaultRef, defaultTestCmd string
	if binding != nil {
		defaultRef = binding.DefaultRef
		defaultTestCmd = binding.DefaultTestCmd
	}

	var isInvestigation bool
	if comp, err := s.slackCompleter(); err == nil {
		isInvestigation = investigationHint(ctx, comp, instruction)
	}

	result, err := s.planner.SubmitPlan(ctx, planner.PlanRequest{
		OrgID:             inst.OrgID,
		UserID:            userID,
		Task:              instruction,
		RepoURL:           repoURL,
		Ref:               defaultRef,
		TestCmd:           defaultTestCmd, // empty is fine: pkg/daemon infers it (see infer.go)
		InvestigationOnly: isInvestigation,
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

// investigationHint asks whether the instruction reads like "investigate/
// find out/explain" rather than "fix/add/change" — a cheap, non-binding
// signal passed to the Architect as InvestigationOnly. Wrong in either
// direction is not costly: false gives the Architect no permission to skip
// the diff it would still be free to conclude isn't needed on the object
// task facts anyway, and true still only hints — the Architect is the one
// that actually decides via NoDiffExpected.
func investigationHint(ctx context.Context, complete completeFunc, instruction string) bool {
	system := `Respond with ONLY JSON: {"investigation_only": true|false}. ` +
		`True if the instruction only asks to investigate, explain, or report — false if it asks for a code change.`
	resp, err := complete(ctx, system, instruction)
	if err != nil {
		return false
	}
	start, end := strings.IndexByte(resp, '{'), strings.LastIndexByte(resp, '}')
	if start == -1 || end == -1 {
		return false
	}
	var out struct {
		InvestigationOnly bool `json:"investigation_only"`
	}
	json.Unmarshal([]byte(resp[start:end+1]), &out)
	return out.InvestigationOnly
}
