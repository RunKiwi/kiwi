// SPDX-License-Identifier: LicenseRef-Kiwi-BSL-1.1
// Copyright (c) 2026 RunKiwi. Licensed under the Business Source License 1.1.
// See ee/LICENSE. This is Control Plane code and is NOT Apache-2.0.

package orchestrator

import (
	"testing"

	"github.com/ibreakthecloud/kiwi/ee/auth"
	"github.com/ibreakthecloud/kiwi/pkg/store"
)

// instructionFromSlack is the sanitizer fetchSlackContext now reuses on
// every history message (see slack_context.go) so an old test:"..."/
// repo:owner/name token sitting in channel or thread history can't reach
// the Architect as literal task text. Exercised directly here since that's
// the load-bearing unit — the wiring in fetchSlackContext is then a single
// obviously-correct call to it.
func TestInstructionFromSlackStripsMentionAndInlineOverrides(t *testing.T) {
	got := instructionFromSlack(`<@U0BOT> fix the bug repo:acme/widget test:"go test ./..."`)
	if got != "fix the bug" {
		t.Fatalf("got %q, want the mention and both inline overrides stripped", got)
	}
}

func TestInstructionFromSlackLeavesPlainTextUntouched(t *testing.T) {
	got := instructionFromSlack("investigate the flaky login test")
	if got != "investigate the flaky login test" {
		t.Fatalf("got %q, want plain text unchanged", got)
	}
}

func TestHandleSlackTriggerSubmitsAPlanWhenChannelIsBound(t *testing.T) {
	s := newTestServer(t)
	ctx := t.Context()

	if err := s.db.WithContext(ctx).Create(&auth.Organization{ID: "org_1", Plan: "pro"}).Error; err != nil {
		t.Fatalf("seed org: %v", err)
	}
	_ = s.storage.UpsertSlackInstallation(ctx, &store.SlackInstallation{TeamID: "T1", OrgID: "org_1"})
	_ = s.storage.SetSlackBotToken(ctx, "T1", "xoxb-test")
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
	if tasks[0].Origin != store.OriginSlack {
		t.Errorf("Origin = %q, want %q — every task a Slack @mention creates must be attributable to Slack", tasks[0].Origin, store.OriginSlack)
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

// Regression test: a channel binding's DefaultModel/DefaultArchitectModel
// must win over SubmitPlan's own runtime auto-pick, the same priority
// DefaultTestCmd already has over inference.
func TestHandleSlackTriggerUsesTheBoundChannelsConfiguredModels(t *testing.T) {
	s := newTestServer(t)
	ctx := t.Context()

	if err := s.db.WithContext(ctx).Create(&auth.Organization{ID: "org_1", Plan: "pro"}).Error; err != nil {
		t.Fatalf("seed org: %v", err)
	}
	_ = s.storage.UpsertSlackInstallation(ctx, &store.SlackInstallation{TeamID: "T1", OrgID: "org_1"})
	_ = s.storage.SetSlackBotToken(ctx, "T1", "xoxb-test")
	_ = s.storage.SaveCredential(ctx, "org_1", "ANTHROPIC_API_KEY", store.CredentialLLM, "sk-ant-test")
	_ = s.storage.UpsertGitHubInstallation(ctx, &store.GitHubInstallation{InstallationID: 1, OrgID: "org_1", AccountLogin: "acme"})
	// Deliberately NOT equal to DefaultWorkerModel (the auto-pick's own
	// last-resort fallback): using the same value would make this
	// assertion pass whether or not the binding's model was actually
	// read, since the auto-pick would land on the same string by
	// coincidence with an empty catalog.
	_ = s.storage.CreateSlackChannelBinding(ctx, &store.SlackChannelBinding{
		OrgID: "org_1", TeamID: "T1", ChannelID: "C1", RepoURL: "https://github.com/acme/widget",
		DefaultModel: "claude-3-5-haiku-20241022",
	})

	s.handleSlackTrigger(ctx, "T1", "C1", "", "100.001", "U1", "<@U0BOT> fix the login bug")

	var tasks []store.QueuedTask
	s.db.WithContext(ctx).Where("org_id = ?", "org_1").Find(&tasks)
	if len(tasks) != 1 {
		t.Fatalf("expected exactly one queued task, got %d", len(tasks))
	}
	if got := tasks[0].Spec["model"]; got != "claude-3-5-haiku-20241022" {
		t.Fatalf("Spec[model] = %v, want the channel binding's configured model, not an auto-picked one", got)
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
	_ = s.storage.SetSlackBotToken(ctx, "T1", "xoxb-test")
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

// Regression test for the missing test:-override syntax: a repo with no
// inferable test convention (e.g. a docs/marketing site) and no channel
// binding default previously had no way to give a Slack task a test
// command at all. An inline test:"..." token must reach the submitted
// task's spec.
func TestHandleSlackTriggerHonorsInlineTestCmdOverride(t *testing.T) {
	s := newTestServer(t)
	ctx := t.Context()

	if err := s.db.WithContext(ctx).Create(&auth.Organization{ID: "org_1", Plan: "pro"}).Error; err != nil {
		t.Fatalf("seed org: %v", err)
	}
	_ = s.storage.UpsertSlackInstallation(ctx, &store.SlackInstallation{TeamID: "T1", OrgID: "org_1"})
	_ = s.storage.SetSlackBotToken(ctx, "T1", "xoxb-test")
	_ = s.storage.SaveCredential(ctx, "org_1", "ANTHROPIC_API_KEY", store.CredentialLLM, "sk-ant-test")
	_ = s.storage.UpsertGitHubInstallation(ctx, &store.GitHubInstallation{InstallationID: 1, OrgID: "org_1", AccountLogin: "acme"})
	_ = s.storage.CreateSlackChannelBinding(ctx, &store.SlackChannelBinding{
		OrgID: "org_1", TeamID: "T1", ChannelID: "C1", RepoURL: "https://github.com/acme/website",
		// No DefaultTestCmd: the binding alone gives the daemon nothing to
		// verify with in a repo where infer.go can't guess a convention.
	})

	s.handleSlackTrigger(ctx, "T1", "C1", "", "100.001", "U1", `<@U0BOT> fix the broken link test:"npm run test:links"`)

	var tasks []store.QueuedTask
	s.db.WithContext(ctx).Where("org_id = ?", "org_1").Find(&tasks)
	if len(tasks) != 1 {
		t.Fatalf("expected exactly one queued task, got %d", len(tasks))
	}
	if got := tasks[0].Spec["test_cmd"]; got != "npm run test:links" {
		t.Fatalf("test_cmd = %v, want the inline override", got)
	}
	// The raw test:"..." token is a directive to Kiwi's Slack layer, not part
	// of the task — it must not reach the Architect as if it were the ask.
	if got := tasks[0].Spec["task"]; got != "fix the broken link" {
		t.Fatalf("task = %v, want the test: token stripped out", got)
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
