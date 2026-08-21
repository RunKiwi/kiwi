// SPDX-License-Identifier: LicenseRef-Kiwi-BSL-1.1
// Copyright (c) 2026 RunKiwi. Licensed under the Business Source License 1.1.
// See ee/LICENSE. This is Control Plane code and is NOT Apache-2.0.

package orchestrator

import (
	"testing"

	"github.com/ibreakthecloud/kiwi/ee/auth"
	"github.com/ibreakthecloud/kiwi/pkg/store"
)

func TestHandleSlackTriggerSubmitsAPlanWhenChannelIsBound(t *testing.T) {
	s := newTestServer(t)
	ctx := t.Context()

	if err := s.db.WithContext(ctx).Create(&auth.Organization{ID: "org_1", Plan: "pro"}).Error; err != nil {
		t.Fatalf("seed org: %v", err)
	}
	_ = s.storage.UpsertSlackInstallation(ctx, &store.SlackInstallation{TeamID: "T1", OrgID: "org_1"})
	_ = s.storage.SaveCredential(ctx, "org_1", "SLACK_BOT_TOKEN", store.CredentialSlack, "xoxb-test")
	_ = s.storage.SaveCredential(ctx, "org_1", "ANTHROPIC_API_KEY", store.CredentialLLM, "sk-ant-test")
	_ = s.storage.UpsertGitHubInstallation(ctx, &store.GitHubInstallation{InstallationID: 1, OrgID: "org_1", AccountLogin: "acme"})
	_ = s.storage.CreateSlackChannelBinding(ctx, &store.SlackChannelBinding{
		OrgID: "org_1", TeamID: "T1", ChannelID: "C1", RepoURL: "https://github.com/acme/widget",
	})

	s.handleSlackTrigger(ctx, "T1", "C1", "" /* threadTS: fresh top-level mention */, "100.001", "U1", "<@U0BOT> fix the login bug")

	var tasks []store.QueuedTask
	s.db.WithContext(ctx).Where("org_id = ?", "org_1").Find(&tasks)
	if len(tasks) != 1 {
		t.Fatalf("expected exactly one queued task, got %d", len(tasks))
	}
	if tasks[0].Spec["repo_url"] != "https://github.com/acme/widget" {
		t.Fatalf("got spec %+v", tasks[0].Spec)
	}

	var rows []store.SlackTriggeredTask
	s.db.WithContext(ctx).Where("org_id = ?", "org_1").Find(&rows)
	if len(rows) != 1 || rows[0].QueuedTaskID == "" {
		t.Fatalf("expected a SlackTriggeredTask row linking the thread to the task, got %v", rows)
	}
	// The regression this covers: for a fresh top-level mention (no thread
	// yet), ThreadTS must be the mention message's own ts — not something
	// else like a status-message ts — so that a human reply in the resulting
	// Slack thread (which Slack tags with thread_ts == the mention's ts)
	// actually matches this row via LatestSlackTriggeredTask.
	if rows[0].ThreadTS != "100.001" {
		t.Fatalf("ThreadTS = %q, want the mention message's own ts (100.001) so a reply in that thread matches this row", rows[0].ThreadTS)
	}
}

// End-to-end proof of the ThreadTS fix: a fresh mention starts a thread
// rooted at its own message ts, and a human reply in THAT thread (which Slack
// tags with thread_ts == the original mention's ts, exactly what a real reply
// carries) must be recognized as continuing that thread rather than treated
// as an unrelated new trigger.
func TestHandleSlackTriggerReplyInTheResultingThreadIsRecognized(t *testing.T) {
	s := newTestServer(t)
	ctx := t.Context()

	if err := s.db.WithContext(ctx).Create(&auth.Organization{ID: "org_1", Plan: "pro"}).Error; err != nil {
		t.Fatalf("seed org: %v", err)
	}
	_ = s.storage.UpsertSlackInstallation(ctx, &store.SlackInstallation{TeamID: "T1", OrgID: "org_1"})
	_ = s.storage.SaveCredential(ctx, "org_1", "SLACK_BOT_TOKEN", store.CredentialSlack, "xoxb-test")
	_ = s.storage.SaveCredential(ctx, "org_1", "ANTHROPIC_API_KEY", store.CredentialLLM, "sk-ant-test")
	_ = s.storage.UpsertGitHubInstallation(ctx, &store.GitHubInstallation{InstallationID: 1, OrgID: "org_1", AccountLogin: "acme"})
	_ = s.storage.CreateSlackChannelBinding(ctx, &store.SlackChannelBinding{
		OrgID: "org_1", TeamID: "T1", ChannelID: "C1", RepoURL: "https://github.com/acme/widget",
	})

	const mentionTS = "100.001"
	s.handleSlackTrigger(ctx, "T1", "C1", "", mentionTS, "U1", "<@U0BOT> fix the login bug")

	var before []store.SlackTriggeredTask
	s.db.WithContext(ctx).Where("org_id = ?", "org_1").Find(&before)
	if len(before) != 1 {
		t.Fatalf("expected one SlackTriggeredTask row after the first trigger, got %d", len(before))
	}

	// A reply in that same thread: threadTS is the mention's own ts, exactly
	// as Slack would deliver it.
	s.handleSlackTrigger(ctx, "T1", "C1", mentionTS, "100.002", "U1", "<@U0BOT> also handle the empty-string case")

	// It must have gone through handleSlackThreadReply's classification path
	// (which — with no completer configured in this test — falls back to
	// ambiguous and posts interactive buttons rather than silently
	// submitting a second, unrelated task) rather than handleSlackTrigger's
	// own fresh-trigger path creating an unrelated second row against a
	// different thread root.
	var after []store.SlackTriggeredTask
	s.db.WithContext(ctx).Where("org_id = ?", "org_1").Find(&after)
	for _, row := range after {
		if row.ThreadTS != mentionTS {
			t.Fatalf("found a SlackTriggeredTask row rooted at %q, want everything rooted at the original mention %q — the reply was not recognized as continuing the same thread", row.ThreadTS, mentionTS)
		}
	}
}

func TestHandleSlackTriggerNoOpsWhenNoInstallationForTeam(t *testing.T) {
	s := newTestServer(t)
	ctx := t.Context()
	// No UpsertSlackInstallation call: unknown team.
	s.handleSlackTrigger(ctx, "T-unknown", "C1", "", "100.001", "U1", "fix the bug")

	var tasks []store.QueuedTask
	s.db.WithContext(ctx).Find(&tasks)
	if len(tasks) != 0 {
		t.Fatalf("expected no task submitted for an unrecognized team, got %d", len(tasks))
	}
}
