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

// instructionFromSlack also strips repo:/test: override tokens, not just the
// mention: both are directives to Kiwi's Slack layer about which repo/test
// command to use, not part of the task itself, and left in they read to the
// Architect like part of the ask — worst for test:"<command>", a quoted
// shell command that looks exactly like an instruction rather than the
// verification guard it actually is (see CLAUDE.md's task-vs-guard
// distinction).
func instructionFromSlack(text string) string {
	stripped := slackMentionRe.ReplaceAllString(text, "")
	stripped = inlineOverrideRe.ReplaceAllString(stripped, "")
	stripped = inlineTestCmdRe.ReplaceAllString(stripped, "")
	return strings.TrimSpace(strings.Join(strings.Fields(stripped), " "))
}

// inlineTestCmdRe matches test:"<command>" anywhere in the message — the
// only way to set a test command per-task rather than per-channel. Without
// it the sole source is a channel binding's DefaultTestCmd, set once by an
// admin, so a repo with no inferable test convention (pkg/daemon's infer.go)
// can never be given one from Slack at all. Quoted because unlike repo:'s
// owner/name token, a test command routinely contains spaces.
var inlineTestCmdRe = regexp.MustCompile(`test:"([^"]+)"`)

func inlineTestCmdOverride(text string) (string, bool) {
	m := inlineTestCmdRe.FindStringSubmatch(text)
	if m == nil {
		return "", false
	}
	return m[1], true
}

// handleSlackTrigger turns an @mention into a task. Every path returns
// without an error and answers nothing back to the caller beyond a log line
// — the webhook handler has already answered Slack, matching
// handleCommentTrigger's "a refusal is a log line, never a failed
// delivery" posture.
//
// messageTS is the @mention event's own ts — distinct from threadTS, which
// Slack leaves empty for a fresh top-level mention and sets to the root's ts
// for a reply inside an existing conversation. Both are needed: the eyes
// reaction always attaches to the specific message that was mentioned
// (messageTS), while everything Kiwi posts back threads into
// firstNonEmpty(threadTS, messageTS) — the existing conversation if there is
// one, otherwise a new thread rooted at the mention itself. Getting this
// wrong (using threadTS alone) was a real bug: for every fresh mention the
// reaction silently failed on an empty timestamp, the status reply posted as
// an unthreaded new message instead of a reply, and the persisted
// SlackTriggeredTask.ThreadTS ended up pointing at that stray status
// message's own ts rather than the mention's — so a human reply in the
// thread under their actual @mention could never match it, and
// continue/fork/new classification never fired.
func (s *Server) handleSlackTrigger(ctx context.Context, teamID, channelID, threadTS, messageTS, userID, text string) {
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

	token, err := inst.DecryptBotToken()
	if err != nil || token == "" {
		log.Printf("[slackapp] org %s has an installation but no bot token", inst.OrgID)
		return
	}

	// The thread everything from here on replies into: the existing
	// conversation if this mention landed inside one, otherwise a new thread
	// rooted at the mention message itself.
	replyThreadTS := firstNonEmpty(threadTS, messageTS)

	rawInstruction := instructionFromSlack(text)
	if rawInstruction == "" {
		return
	}
	// Context assembly cares about the ORIGINAL threadTS, not replyThreadTS:
	// an empty threadTS means "no thread exists yet", which is exactly what
	// tells fetchSlackContext to pull channel history instead of trying to
	// fetch replies for a thread that doesn't exist.
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
			s.slackClient.PostMessage(ctx, token, channelID, replyThreadTS, ambiguousReply)
		}
		return
	}

	if s.slackClient != nil {
		s.slackClient.AddReaction(ctx, token, channelID, messageTS, "eyes")
	}

	// An inline test:"..." token outranks the channel's default, the same
	// priority repo: takes over a bound repo.
	testCmdOverride, _ := inlineTestCmdOverride(text)
	defaults := slackBindingDefaults(binding, inst.OrgID, testCmdOverride)

	var isInvestigation bool
	if comp, err := s.slackCompleter(ctx); err == nil {
		isInvestigation = investigationHint(ctx, comp, instruction)
	}

	result, err := s.planner.SubmitPlan(ctx, planner.PlanRequest{
		OrgID:             inst.OrgID,
		UserID:            userID,
		Task:              instruction,
		RepoURL:           repoURL,
		Ref:               defaults.ref,
		TestCmd:           defaults.testCmd, // empty is fine: pkg/daemon infers it (see infer.go)
		InvestigationOnly: isInvestigation,
		Origin:            store.OriginSlack,
		// Empty leaves both up to SubmitPlan's own default resolution — the
		// runtime Kiwi-funded catalog auto-pick for Model (defaultWorkerModelFor)
		// and the architect split default for ArchitectModel. A channel that
		// configured either always wins over that auto-pick.
		Model:          defaults.model,
		ArchitectModel: defaults.architectModel,
	})
	if err != nil {
		if s.slackClient != nil {
			s.slackClient.PostMessage(ctx, token, channelID, replyThreadTS, fmt.Sprintf("Couldn't start that task: %s", err.Error()))
		}
		return
	}

	statusText := fmt.Sprintf("Working on it — job `%s`.", result.JobID)
	if result.Warning != "" {
		statusText += " " + result.Warning
	}
	statusTS := ""
	if s.slackClient != nil {
		statusTS, err = s.slackClient.PostMessage(ctx, token, channelID, replyThreadTS, statusText)
		if err != nil {
			log.Printf("[slackapp] posting status message for job %s: %v", result.JobID, err)
		}
	}

	row := &store.SlackTriggeredTask{
		OrgID: inst.OrgID, TeamID: teamID, ChannelID: channelID, ThreadTS: replyThreadTS,
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
