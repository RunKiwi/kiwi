// ee/orchestrator/slack_webhook.go
// SPDX-License-Identifier: LicenseRef-Kiwi-BSL-1.1
// Copyright (c) 2026 RunKiwi. Licensed under the Business Source License 1.1.
// See ee/LICENSE. This is Control Plane code and is NOT Apache-2.0.

package orchestrator

import (
	"context"
	"io"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/ibreakthecloud/kiwi/ee/slackapp"
)

// handleSlackWebhook serves POST /api/v1/webhooks/slack/events — Slack's
// Events API and interactivity deliveries share one URL in Slack's own
// console, so both are handled here and dispatched by content type, the
// same shape github_webhook.go uses to dispatch by X-GitHub-Event.
func (s *Server) handleSlackWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	secret := os.Getenv("SLACK_SIGNING_SECRET")
	if secret == "" {
		log.Println("[slackapp] SLACK_SIGNING_SECRET not set; rejecting webhook (fail closed)")
		http.Error(w, "webhook not configured", http.StatusServiceUnavailable)
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "failed to read body", http.StatusBadRequest)
		return
	}

	ts := r.Header.Get("X-Slack-Request-Timestamp")
	sig := r.Header.Get("X-Slack-Signature")
	if !slackapp.VerifySignature(secret, ts, body, sig) {
		http.Error(w, "invalid signature", http.StatusUnauthorized)
		return
	}

	// Interactivity deliveries are form-encoded; Events API deliveries are
	// raw JSON. Slack sets Content-Type accordingly, so branch on that
	// rather than trying to parse both shapes against every body.
	if ct := r.Header.Get("Content-Type"); len(ct) >= len("application/x-www-form-urlencoded") &&
		ct[:len("application/x-www-form-urlencoded")] == "application/x-www-form-urlencoded" {
		s.handleSlackInteractivity(r.Context(), body)
		w.WriteHeader(http.StatusOK)
		return
	}

	ev, ok := slackapp.ParseEvent(body)
	if !ok {
		w.WriteHeader(http.StatusOK) // unrecognized shape: not an error, nothing to do
		return
	}

	if ev.Type == "url_verification" {
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(ev.Challenge))
		return
	}

	if ev.EventType == "app_mention" {
		// Handled off the request goroutine, same posture as
		// maybeAssembleRecord in daemon_api.go: Slack expects a fast 200 and
		// retries a delivery that doesn't get one within 3 seconds. Must use a
		// detached context, not r.Context() — net/http cancels the request's
		// context the moment this handler returns, which happens immediately
		// after this goroutine is spawned, so r.Context() would be canceled
		// before the trigger pipeline's SubmitPlan/DB/Slack API calls ever run.
		go func(teamID, channelID, threadTS, messageTS, userID, text string) {
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			s.handleSlackTrigger(ctx, teamID, channelID, threadTS, messageTS, userID, text)
		}(ev.TeamID, ev.ChannelID, ev.ThreadTS, ev.TS, ev.UserID, ev.Text)
	}
	w.WriteHeader(http.StatusOK)
}
