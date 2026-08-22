// SPDX-License-Identifier: LicenseRef-Kiwi-BSL-1.1
// Copyright (c) 2026 RunKiwi. Licensed under the Business Source License 1.1.
// See ee/LICENSE. This is Control Plane code and is NOT Apache-2.0.

package orchestrator

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/ibreakthecloud/kiwi/ee/auth"
	"github.com/ibreakthecloud/kiwi/ee/slackapp"
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

// Regression test: a "new" (unrelated) task started inside an already-
// actioned thread — whether via handleSlackThreadReply's own classification
// or a "New task" button click — used to skip the channel binding lookup
// entirely, so a channel that pinned a worker/architect model silently lost
// that pin the moment the request wasn't a fresh top-level @mention.
// slackBindingDefaults is the shared fix; exercised here with no database,
// mirroring buildContinuationTask's "no I/O" testability.
func TestSlackBindingDefaultsAppliesTheBoundChannelsConfiguredModels(t *testing.T) {
	binding := &store.SlackChannelBinding{
		OrgID: "org_1", DefaultTestCmd: "go test ./...",
		DefaultModel: "claude-haiku-4-5-20251001", DefaultArchitectModel: "claude-opus-4-8",
	}
	got := slackBindingDefaults(binding, "org_1", "")
	if got.testCmd != "go test ./..." || got.model != "claude-haiku-4-5-20251001" || got.architectModel != "claude-opus-4-8" {
		t.Fatalf("got %+v, want the binding's own defaults", got)
	}
}

// An explicit test:"..." override still outranks the channel's own default —
// the same priority handleSlackTrigger already gives it.
func TestSlackBindingDefaultsExplicitTestCmdOverridesTheBinding(t *testing.T) {
	binding := &store.SlackChannelBinding{OrgID: "org_1", DefaultTestCmd: "go test ./...", DefaultModel: "claude-haiku-4-5-20251001"}
	got := slackBindingDefaults(binding, "org_1", "make test")
	if got.testCmd != "make test" {
		t.Fatalf("testCmd = %q, want the explicit override to win", got.testCmd)
	}
	if got.model != "claude-haiku-4-5-20251001" {
		t.Fatalf("model = %q, want the binding's model unaffected by the test-cmd override", got.model)
	}
}

// A binding that belongs to a different org (surviving a workspace
// re-install under a different org) must be treated as absent — the same
// stale-binding guard handleSlackTrigger applies before this helper runs.
func TestSlackBindingDefaultsIgnoresABindingFromAnotherOrg(t *testing.T) {
	binding := &store.SlackChannelBinding{OrgID: "org_victim", DefaultModel: "claude-haiku-4-5-20251001"}
	got := slackBindingDefaults(binding, "org_1", "")
	if got.model != "" || got.testCmd != "" || got.architectModel != "" {
		t.Fatalf("got %+v, want all-empty defaults for a binding belonging to a different org", got)
	}
}

// No binding at all (never configured) must also degrade to all-empty, which
// is what lets SubmitPlan's own runtime auto-pick take over.
func TestSlackBindingDefaultsHandlesANilBinding(t *testing.T) {
	got := slackBindingDefaults(nil, "org_1", "")
	if got.model != "" || got.testCmd != "" || got.architectModel != "" {
		t.Fatalf("got %+v, want all-empty defaults for no binding", got)
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
	_ = s.storage.SetSlackBotToken(ctx, "T-attacker", "xoxb-attacker")

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

// End-to-end proof that a "New task" button click honors the channel's
// configured model, not just handleSlackTrigger's fresh-mention path.
func TestHandleSlackInteractivityNewTaskUsesTheBoundChannelsConfiguredModel(t *testing.T) {
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

	var parent store.QueuedTask
	parent.ID = "task_parent"
	parent.OrgID = "org_1"
	parent.JobID = "job_parent"
	parent.Spec = map[string]interface{}{"repo_url": "https://github.com/acme/widget"}
	if err := s.db.WithContext(ctx).Create(&parent).Error; err != nil {
		t.Fatalf("seed parent task: %v", err)
	}
	_ = s.storage.CreateSlackTriggeredTask(ctx, &store.SlackTriggeredTask{
		ID: "stt_1", OrgID: "org_1", TeamID: "T1", ChannelID: "C1", ThreadTS: "100.001", QueuedTaskID: "task_parent",
	})

	s.slackClient = slackapp.New(
		slackapp.WithBaseURL("https://slack.com/api"),
		slackapp.WithHTTPClient(&http.Client{
			Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Content-Type": []string{"application/json"}},
					Body:       io.NopCloser(strings.NewReader(`{"ok":true,"ts":"100.002"}`)),
				}, nil
			}),
		}),
	)

	form := interactivityFormBody(t, "T1", "C1", "slack_thread_new", "stt_1|do something unrelated")
	s.handleSlackInteractivity(ctx, form)

	var tasks []store.QueuedTask
	s.db.WithContext(ctx).Where("org_id = ? AND id != ?", "org_1", "task_parent").Find(&tasks)
	if len(tasks) != 1 {
		t.Fatalf("expected exactly one new queued task, got %d", len(tasks))
	}
	if got := tasks[0].Spec["model"]; got != "claude-3-5-haiku-20241022" {
		t.Fatalf("Spec[model] = %v, want the channel binding's configured model", got)
	}
}

// Regression test for the silent-failure bug: with no GEMINI platform key
// configured (the default in this test harness), s.slackCompleter() fails.
// Before the fix, handleSlackThreadReply returned immediately on that error
// and posted nothing at all. It must instead degrade to the ambiguous
// interactive-buttons prompt, the same as when classification itself fails.
func TestHandleSlackThreadReplyPostsAmbiguousButtonsWhenCompleterUnavailable(t *testing.T) {
	t.Setenv("KIWI_PLATFORM_GEMINI_API_KEY", "") // pin: this test exercises the slackCompleter()-fails path specifically

	s := newTestServer(t)
	ctx := t.Context()

	_ = s.storage.UpsertSlackInstallation(ctx, &store.SlackInstallation{TeamID: "T1", OrgID: "org_1"})
	_ = s.storage.SetSlackBotToken(ctx, "T1", "xoxb-test")

	var parent store.QueuedTask
	parent.ID = "task_1"
	parent.OrgID = "org_1"
	parent.JobID = "job_1"
	if err := s.db.WithContext(ctx).Create(&parent).Error; err != nil {
		t.Fatalf("seed parent task: %v", err)
	}
	existing := &store.SlackTriggeredTask{ID: "stt_1", OrgID: "org_1", TeamID: "T1", ChannelID: "C1", ThreadTS: "100.001", QueuedTaskID: "task_1"}
	if err := s.storage.CreateSlackTriggeredTask(ctx, existing); err != nil {
		t.Fatalf("seed triggered task: %v", err)
	}

	var posted []map[string]interface{}
	s.slackClient = slackapp.New(
		slackapp.WithBaseURL("https://slack.com/api"),
		slackapp.WithHTTPClient(&http.Client{
			Transport: roundTripperFunc(func(r *http.Request) (*http.Response, error) {
				if strings.HasSuffix(r.URL.Path, "/chat.postMessage") {
					var body map[string]interface{}
					_ = json.NewDecoder(r.Body).Decode(&body)
					posted = append(posted, body)
				}
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Content-Type": []string{"application/json"}},
					Body:       io.NopCloser(strings.NewReader(`{"ok":true,"ts":"100.002"}`)),
				}, nil
			}),
		}),
	)

	s.handleSlackThreadReply(ctx, "T1", "C1", "100.001", "U1", "<@BOT> also handle nulls", existing)

	if len(posted) != 1 {
		t.Fatalf("expected exactly one chat.postMessage call, got %d: %v", len(posted), posted)
	}
	if _, ok := posted[0]["blocks"]; !ok {
		t.Fatalf("expected the ambiguous interactive-buttons prompt (a message with blocks), got %v", posted[0])
	}
}
