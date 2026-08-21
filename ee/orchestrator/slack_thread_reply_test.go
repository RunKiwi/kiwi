// SPDX-License-Identifier: LicenseRef-Kiwi-BSL-1.1
// Copyright (c) 2026 RunKiwi. Licensed under the Business Source License 1.1.
// See ee/LICENSE. This is Control Plane code and is NOT Apache-2.0.

package orchestrator

import (
	"context"
	"encoding/json"
	"net/url"
	"testing"

	"github.com/ibreakthecloud/kiwi/pkg/store"
)

func TestClassifyThreadReplyContinue(t *testing.T) {
	complete := func(ctx context.Context, system, user string) (string, error) {
		return `{"verdict": "continue"}`, nil
	}
	got, err := classifyThreadReply(context.Background(), complete, "PR #9 fixes the null check", "also handle the empty-string case")
	if err != nil || got != "continue" {
		t.Fatalf("got %q err=%v", got, err)
	}
}

func TestClassifyThreadReplyRejectsUnknownVerdict(t *testing.T) {
	complete := func(ctx context.Context, system, user string) (string, error) {
		return `{"verdict": "something-else"}`, nil
	}
	if _, err := classifyThreadReply(context.Background(), complete, "summary", "message"); err == nil {
		t.Fatal("expected an error for an unrecognized verdict")
	}
}

func interactivityFormBody(t *testing.T, teamID, channelID, actionID, actionValue string) []byte {
	t.Helper()
	payload, err := json.Marshal(map[string]any{
		"type":    "block_actions",
		"team":    map[string]string{"id": teamID},
		"channel": map[string]string{"id": channelID},
		"message": map[string]string{"ts": "100.001"},
		"user":    map[string]string{"id": "U1"},
		"actions": []map[string]string{{"action_id": actionID, "value": actionValue}},
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	return []byte(url.Values{"payload": {string(payload)}}.Encode())
}

// This is the guard for the tenant-isolation gap: action_value is
// client-supplied, and a signed request from ANY installed workspace can
// carry a triggeredTaskID that names a row belonging to a completely
// different team/org. handleSlackInteractivity must refuse to act on it
// rather than trusting the signature alone.
func TestHandleSlackInteractivityRejectsATaskFromAnotherTeam(t *testing.T) {
	s := newTestServer(t)
	ctx := t.Context()

	// The row being targeted belongs to team T-victim / org_victim.
	_ = s.storage.UpsertSlackInstallation(ctx, &store.SlackInstallation{TeamID: "T-victim", OrgID: "org_victim"})
	_ = s.storage.CreateSlackTriggeredTask(ctx, &store.SlackTriggeredTask{
		ID: "stt_victim", OrgID: "org_victim", TeamID: "T-victim", ChannelID: "C-victim",
		ThreadTS: "99.000", QueuedTaskID: "task_victim",
	})
	var victimTask store.QueuedTask
	victimTask.ID = "task_victim"
	victimTask.OrgID = "org_victim"
	victimTask.JobID = "job_victim"
	if err := s.db.WithContext(ctx).Create(&victimTask).Error; err != nil {
		t.Fatalf("seed victim task: %v", err)
	}

	// The attacker's own installed workspace, a completely different team/org.
	_ = s.storage.UpsertSlackInstallation(ctx, &store.SlackInstallation{TeamID: "T-attacker", OrgID: "org_attacker"})
	_ = s.storage.SaveCredential(ctx, "org_attacker", "SLACK_BOT_TOKEN", store.CredentialSlack, "xoxb-attacker")

	// A validly-signed interactivity payload from the attacker's own team,
	// but pointing action_value at the victim's SlackTriggeredTask row.
	form := interactivityFormBody(t, "T-attacker", "C-attacker", "slack_thread_continue", "stt_victim|do something")

	s.handleSlackInteractivity(ctx, form)

	var tasks []store.QueuedTask
	s.db.WithContext(ctx).Where("org_id = ?", "org_attacker").Find(&tasks)
	if len(tasks) != 0 {
		t.Fatalf("expected no task created for org_attacker from a cross-team interactivity payload, got %d", len(tasks))
	}

	var rows []store.SlackTriggeredTask
	s.db.WithContext(ctx).Where("team_id = ?", "T-attacker").Find(&rows)
	if len(rows) != 0 {
		t.Fatalf("expected no SlackTriggeredTask row created for the attacker's team, got %d", len(rows))
	}
}
