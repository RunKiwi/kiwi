// SPDX-License-Identifier: LicenseRef-Kiwi-BSL-1.1
// Copyright (c) 2026 RunKiwi. Licensed under the Business Source License 1.1.
// See ee/LICENSE. This is Control Plane code and is NOT Apache-2.0.

package orchestrator

import (
	"testing"

	"github.com/ibreakthecloud/kiwi/pkg/store"
)

func TestHandleSlackTriggerSubmitsAPlanWhenChannelIsBound(t *testing.T) {
	s := newTestServer(t)
	ctx := t.Context()

	_ = s.storage.UpsertSlackInstallation(ctx, &store.SlackInstallation{TeamID: "T1", OrgID: "org_1"})
	_ = s.storage.SaveCredential(ctx, "org_1", "SLACK_BOT_TOKEN", store.CredentialSlack, "xoxb-test")
	_ = s.storage.SaveCredential(ctx, "org_1", "ANTHROPIC_API_KEY", store.CredentialLLM, "sk-ant-test")
	_ = s.storage.UpsertGitHubInstallation(ctx, &store.GitHubInstallation{InstallationID: 1, OrgID: "org_1", AccountLogin: "acme"})
	_ = s.storage.CreateSlackChannelBinding(ctx, &store.SlackChannelBinding{
		OrgID: "org_1", TeamID: "T1", ChannelID: "C1", RepoURL: "https://github.com/acme/widget",
	})

	s.handleSlackTrigger(ctx, "T1", "C1", "", "U1", "<@U0BOT> fix the login bug")

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
}

func TestHandleSlackTriggerNoOpsWhenNoInstallationForTeam(t *testing.T) {
	s := newTestServer(t)
	ctx := t.Context()
	// No UpsertSlackInstallation call: unknown team.
	s.handleSlackTrigger(ctx, "T-unknown", "C1", "", "U1", "fix the bug")

	var tasks []store.QueuedTask
	s.db.WithContext(ctx).Find(&tasks)
	if len(tasks) != 0 {
		t.Fatalf("expected no task submitted for an unrecognized team, got %d", len(tasks))
	}
}
