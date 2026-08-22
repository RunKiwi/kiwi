// SPDX-License-Identifier: LicenseRef-Kiwi-BSL-1.1
// Copyright (c) 2026 RunKiwi. Licensed under the Business Source License 1.1.
// See ee/LICENSE. This is Control Plane code and is NOT Apache-2.0.

package orchestrator

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/ibreakthecloud/kiwi/ee/auth"
	"github.com/ibreakthecloud/kiwi/pkg/store"
)

// signSlackRequest computes a valid v0 signature the same way
// slackapp.VerifySignature checks one, so this test can exercise the real
// signature-verification path rather than bypassing it.
func signSlackRequest(t *testing.T, secret, ts string, body []byte) string {
	t.Helper()
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte("v0:" + ts + ":" + string(body)))
	return "v0=" + hex.EncodeToString(mac.Sum(nil))
}

func postSlackEvent(t *testing.T, s *Server, secret string, body []byte) {
	t.Helper()
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	req := httptest.NewRequest("POST", "/api/v1/webhooks/slack/events", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Slack-Request-Timestamp", ts)
	req.Header.Set("X-Slack-Signature", signSlackRequest(t, secret, ts, body))
	rec := httptest.NewRecorder()
	s.handleSlackWebhook(rec, req)
	if rec.Code != 200 {
		t.Fatalf("handleSlackWebhook: got status %d, body %s", rec.Code, rec.Body.String())
	}
}

// Regression test for the missing redelivery guard: Slack retries a
// delivery that did not get a 200 within 3 seconds, and again later if that
// retry also fails, so the exact same event_id reaching this handler twice
// is routine. Before RecordSlackEvent gated the app_mention dispatch, each
// retry became a second, independent SubmitPlan for the same mention.
func TestHandleSlackWebhookIgnoresARedeliveredEventID(t *testing.T) {
	s := newTestServer(t)
	ctx := t.Context()
	const secret = "test-signing-secret"
	t.Setenv("SLACK_SIGNING_SECRET", secret)

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

	body := []byte(`{
		"type": "event_callback",
		"event_id": "Ev_REDELIVERED",
		"team_id": "T1",
		"event": {
			"type": "app_mention",
			"user": "U1",
			"text": "<@U0BOT> fix the login bug",
			"channel": "C1",
			"ts": "100.001"
		}
	}`)

	// Two deliveries of the identical event_id, exactly as a Slack retry
	// would send: same body, freshly (re)signed, same event_id inside it.
	postSlackEvent(t, s, secret, body)
	postSlackEvent(t, s, secret, body)

	// RecordSlackEvent runs synchronously in the request handler, before
	// the trigger's goroutine is ever spawned (see handleSlackWebhook) — so
	// asserting on it directly is deterministic, unlike polling for the
	// queued task the goroutine writes: a poll loop that breaks as soon as
	// one task appears could in principle observe the first delivery's task
	// before a second, undeduplicated delivery's goroutine had written its
	// own, and pass while the guard was broken. Claiming the same event_id
	// a third time here proves the guard actually fired for both prior
	// deliveries, not just happens to agree with a task count.
	if fresh, err := s.storage.RecordSlackEvent(ctx, "Ev_REDELIVERED"); err != nil || fresh {
		t.Fatalf("RecordSlackEvent after two deliveries: fresh=%v err=%v, want fresh=false", fresh, err)
	}

	// End-to-end confirmation that the dedup guard actually stopped a
	// second SubmitPlan, not just that RecordSlackEvent's own ledger is
	// correct. The trigger runs off the request goroutine, so wait out the
	// full deadline rather than breaking on the first task seen — the
	// direct RecordSlackEvent assertion above is what proves determinism;
	// this window just gives a real second submission time to land if the
	// guard were broken.
	deadline := time.Now().Add(300 * time.Millisecond)
	var tasks []store.QueuedTask
	for time.Now().Before(deadline) {
		s.db.WithContext(ctx).Where("org_id = ?", "org_1").Find(&tasks)
		time.Sleep(10 * time.Millisecond)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected exactly one queued task from two deliveries of the same event_id, got %d", len(tasks))
	}
}
