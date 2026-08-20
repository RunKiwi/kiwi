// ee/orchestrator/slack_thread_reply.go
// SPDX-License-Identifier: LicenseRef-Kiwi-BSL-1.1
// Copyright (c) 2026 RunKiwi. Licensed under the Business Source License 1.1.
// See ee/LICENSE. This is Control Plane code and is NOT Apache-2.0.

package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"

	"github.com/ibreakthecloud/kiwi/ee/planner"
	"github.com/ibreakthecloud/kiwi/ee/slackapp"
	"github.com/ibreakthecloud/kiwi/pkg/store"
)

const (
	verdictContinue  = "continue"
	verdictFork      = "fork"
	verdictNew       = "new"
	verdictAmbiguous = "ambiguous"
)

// classifyThreadReply asks the Architect-equivalent classifier whether a
// reply in an already-actioned thread continues that work, forks off it,
// starts something unrelated, or is genuinely unclear. No I/O beyond the
// completion call, so the decision logic is table-testable on its own.
func classifyThreadReply(ctx context.Context, complete completeFunc, latestSummary, newMessage string) (string, error) {
	system := "You classify a follow-up message in a thread where Kiwi already produced work. " +
		`Respond with ONLY JSON: {"verdict": "continue|fork|new|ambiguous"}. ` +
		`"continue" if the message refines or extends the same fix. "fork" if it wants a different approach starting from the same work. ` +
		`"new" if it's unrelated. "ambiguous" if you genuinely cannot tell.`
	user := fmt.Sprintf("What Kiwi already did:\n%s\n\nNew message:\n%s", latestSummary, newMessage)
	resp, err := complete(ctx, system, user)
	if err != nil {
		return "", err
	}
	start, end := strings.IndexByte(resp, '{'), strings.LastIndexByte(resp, '}')
	if start == -1 || end == -1 || start >= end {
		return "", fmt.Errorf("no JSON object in classification response")
	}
	var out struct {
		Verdict string `json:"verdict"`
	}
	if err := json.Unmarshal([]byte(resp[start:end+1]), &out); err != nil {
		return "", fmt.Errorf("parse classification response: %w", err)
	}
	switch out.Verdict {
	case verdictContinue, verdictFork, verdictNew, verdictAmbiguous:
		return out.Verdict, nil
	default:
		return "", fmt.Errorf("unrecognized verdict %q", out.Verdict)
	}
}

// summaryForClassification is what the classifier sees as "what Kiwi
// already did" — the parent task's own reported detail (its PR body or
// investigation summary), which is already the most compact accurate
// account of that work that exists anywhere.
func summaryForClassification(parent *store.QueuedTask) string {
	if parent.ResultDetail != nil && *parent.ResultDetail != "" {
		return *parent.ResultDetail
	}
	if parent.Spec != nil {
		if t, ok := parent.Spec["task"].(string); ok {
			return t
		}
	}
	return ""
}

// handleSlackThreadReply is handleSlackTrigger's counterpart for a reply in
// a thread that already has a task: classify, then continue / fork / new /
// ask.
func (s *Server) handleSlackThreadReply(ctx context.Context, teamID, channelID, threadTS, userID, text string, existing *store.SlackTriggeredTask) {
	inst, err := s.storage.GetSlackInstallationByTeamID(ctx, teamID)
	if err != nil || inst == nil {
		return
	}
	token, err := s.storage.GetCredentialPlaintext(ctx, inst.OrgID, "SLACK_BOT_TOKEN")
	if err != nil || token == "" || s.slackClient == nil {
		return
	}

	instruction := instructionFromSlack(text)
	if instruction == "" {
		return
	}

	var parent store.QueuedTask
	if err := s.db.WithContext(ctx).Where("id = ?", existing.QueuedTaskID).First(&parent).Error; err != nil {
		return
	}

	complete, cerr := s.slackCompleter()
	if cerr != nil {
		return
	}
	verdict, err := classifyThreadReply(ctx, complete, summaryForClassification(&parent), instruction)
	if err != nil {
		verdict = verdictAmbiguous
	}

	switch verdict {
	case verdictContinue:
		sessionID := ""
		if sess, serr := s.storage.GetAgentSessionByTask(ctx, inst.OrgID, parent.ID); serr == nil && sess != nil {
			sessionID = sess.ID
		}
		task, err := s.planner.SubmitContinuation(ctx, planner.ContinuationInput{
			OrgID: inst.OrgID, ParentTask: &parent, Instruction: instruction, SessionID: sessionID, Origin: store.OriginPRComment,
		})
		if err != nil {
			s.slackClient.PostMessage(ctx, token, channelID, threadTS, fmt.Sprintf("Couldn't continue that task: %s", err.Error()))
			return
		}
		s.recordSlackThreadTask(ctx, inst.OrgID, teamID, channelID, threadTS, task.ID, token, "Continuing…")

	case verdictFork:
		result, err := s.planner.SubmitFork(ctx, planner.ForkInput{OrgID: inst.OrgID, UserID: userID, ParentTask: &parent, Instruction: instruction})
		if err != nil {
			s.slackClient.PostMessage(ctx, token, channelID, threadTS, fmt.Sprintf("Couldn't fork that task: %s", err.Error()))
			return
		}
		s.recordSlackThreadTask(ctx, inst.OrgID, teamID, channelID, threadTS, firstOf(result.TaskIDs), token, fmt.Sprintf("Forking into a new attempt — job `%s`.", result.JobID))

	case verdictNew:
		repoURL, _ := parent.Spec["repo_url"].(string)
		result, err := s.planner.SubmitPlan(ctx, planner.PlanRequest{OrgID: inst.OrgID, UserID: userID, Task: instruction, RepoURL: repoURL})
		if err != nil {
			s.slackClient.PostMessage(ctx, token, channelID, threadTS, fmt.Sprintf("Couldn't start that task: %s", err.Error()))
			return
		}
		s.recordSlackThreadTask(ctx, inst.OrgID, teamID, channelID, threadTS, firstOf(result.TaskIDs), token, fmt.Sprintf("Starting a new, unrelated task — job `%s`.", result.JobID))

	default: // ambiguous
		s.slackClient.PostInteractiveButtons(ctx, token, channelID, threadTS,
			"Not sure whether that's a continuation, a different approach, or something new — which did you mean?",
			[]slackapp.Button{
				{Label: "Continue", ActionID: "slack_thread_continue", Value: existing.ID + "|" + instruction},
				{Label: "Fork", ActionID: "slack_thread_fork", Value: existing.ID + "|" + instruction},
				{Label: "New task", ActionID: "slack_thread_new", Value: existing.ID + "|" + instruction},
			})
	}
}

func (s *Server) recordSlackThreadTask(ctx context.Context, orgID, teamID, channelID, threadTS, taskID, token, statusText string) {
	statusTS, err := s.slackClient.PostMessage(ctx, token, channelID, threadTS, statusText)
	if err != nil {
		log.Printf("[slackapp] posting status message: %v", err)
	}
	row := &store.SlackTriggeredTask{OrgID: orgID, TeamID: teamID, ChannelID: channelID, ThreadTS: threadTS, QueuedTaskID: taskID, StatusMessageTS: statusTS, LastStatus: "running"}
	if err := s.storage.CreateSlackTriggeredTask(ctx, row); err != nil {
		log.Printf("[slackapp] persist triggered-task row for task %s: %v", taskID, err)
	}
}

var errUnhandledInteraction = errors.New("unhandled interaction")

// handleSlackInteractivity replaces Task 6's placeholder: it now resolves a
// continue/fork/new button click back to the ambiguous case above.
func (s *Server) handleSlackInteractivity(ctx context.Context, formBody []byte) {
	in, ok := slackapp.ParseInteractivity(formBody)
	if !ok {
		return
	}
	parts := strings.SplitN(in.ActionValue, "|", 2)
	if len(parts) != 2 {
		return
	}
	triggeredTaskID, instruction := parts[0], parts[1]

	var existing store.SlackTriggeredTask
	if err := s.db.WithContext(ctx).Where("id = ?", triggeredTaskID).First(&existing).Error; err != nil {
		return
	}
	var parent store.QueuedTask
	if err := s.db.WithContext(ctx).Where("id = ?", existing.QueuedTaskID).First(&parent).Error; err != nil {
		return
	}

	inst, err := s.storage.GetSlackInstallationByTeamID(ctx, in.TeamID)
	if err != nil || inst == nil {
		return
	}
	token, err := s.storage.GetCredentialPlaintext(ctx, inst.OrgID, "SLACK_BOT_TOKEN")
	if err != nil || token == "" {
		return
	}

	switch in.ActionID {
	case "slack_thread_continue":
		task, err := s.planner.SubmitContinuation(ctx, planner.ContinuationInput{OrgID: inst.OrgID, ParentTask: &parent, Instruction: instruction})
		if err == nil {
			s.recordSlackThreadTask(ctx, inst.OrgID, in.TeamID, in.ChannelID, existing.ThreadTS, task.ID, token, "Continuing…")
		}
	case "slack_thread_fork":
		result, err := s.planner.SubmitFork(ctx, planner.ForkInput{OrgID: inst.OrgID, ParentTask: &parent, Instruction: instruction})
		if err == nil {
			s.recordSlackThreadTask(ctx, inst.OrgID, in.TeamID, in.ChannelID, existing.ThreadTS, firstOf(result.TaskIDs), token, fmt.Sprintf("Forking — job `%s`.", result.JobID))
		}
	case "slack_thread_new":
		repoURL, _ := parent.Spec["repo_url"].(string)
		result, err := s.planner.SubmitPlan(ctx, planner.PlanRequest{OrgID: inst.OrgID, Task: instruction, RepoURL: repoURL})
		if err == nil {
			s.recordSlackThreadTask(ctx, inst.OrgID, in.TeamID, in.ChannelID, existing.ThreadTS, firstOf(result.TaskIDs), token, fmt.Sprintf("Starting a new task — job `%s`.", result.JobID))
		}
	}
}
