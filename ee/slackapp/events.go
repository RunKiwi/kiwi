// ee/slackapp/events.go
// SPDX-License-Identifier: LicenseRef-Kiwi-BSL-1.1
// Copyright (c) 2026 RunKiwi. Licensed under the Business Source License 1.1.
// See ee/LICENSE. This is Control Plane code and is NOT Apache-2.0.

package slackapp

import (
	"encoding/json"
	"net/url"
	"strings"
)

// Event is a Slack Events API delivery, reduced to what a trigger needs.
// Everything here is a pure function over the wire payload, on purpose — the
// same reasoning ee/orchestrator/pr_comment.go gives: cheap to table-test,
// no DB or network involved in deciding what an event even is.
type Event struct {
	Type      string // "url_verification" or "event_callback"
	Challenge string // set only for url_verification
	TeamID    string
	EventType string // "app_mention", "message", ...
	UserID    string
	Text      string
	ChannelID string
	TS        string
	ThreadTS  string // empty when the message is not in a thread
}

type eventPayload struct {
	Type      string `json:"type"`
	Challenge string `json:"challenge"`
	TeamID    string `json:"team_id"`
	Event     struct {
		Type     string `json:"type"`
		User     string `json:"user"`
		Text     string `json:"text"`
		Channel  string `json:"channel"`
		TS       string `json:"ts"`
		ThreadTS string `json:"thread_ts"`
	} `json:"event"`
}

// ParseEvent extracts an Event from a webhook body, reporting false when the
// payload is not a shape this integration recognizes.
func ParseEvent(body []byte) (Event, bool) {
	var p eventPayload
	if err := json.Unmarshal(body, &p); err != nil {
		return Event{}, false
	}
	switch p.Type {
	case "url_verification":
		if p.Challenge == "" {
			return Event{}, false
		}
		return Event{Type: p.Type, Challenge: p.Challenge}, true
	case "event_callback":
		if p.Event.Type == "" {
			return Event{}, false
		}
		return Event{
			Type:      p.Type,
			TeamID:    p.TeamID,
			EventType: p.Event.Type,
			UserID:    p.Event.User,
			Text:      p.Event.Text,
			ChannelID: p.Event.Channel,
			TS:        p.Event.TS,
			ThreadTS:  p.Event.ThreadTS,
		}, true
	default:
		return Event{}, false
	}
}

// Interactivity is a Block Kit button click (the continue/fork/new
// disambiguation prompt), delivered as application/x-www-form-urlencoded
// with the real JSON in a single "payload" field.
type Interactivity struct {
	Type        string // "block_actions"
	TeamID      string
	ChannelID   string
	MessageTS   string
	UserID      string
	ActionID    string
	ActionValue string
}

type interactivityPayload struct {
	Type string `json:"type"`
	Team struct {
		ID string `json:"id"`
	} `json:"team"`
	Channel struct {
		ID string `json:"id"`
	} `json:"channel"`
	Message struct {
		TS string `json:"ts"`
	} `json:"message"`
	User struct {
		ID string `json:"id"`
	} `json:"user"`
	Actions []struct {
		ActionID string `json:"action_id"`
		Value    string `json:"value"`
	} `json:"actions"`
}

// ParseInteractivity extracts a button click from an interactivity delivery.
func ParseInteractivity(formBody []byte) (Interactivity, bool) {
	values, err := url.ParseQuery(string(formBody))
	if err != nil {
		return Interactivity{}, false
	}
	raw := values.Get("payload")
	if raw == "" {
		return Interactivity{}, false
	}
	var p interactivityPayload
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		return Interactivity{}, false
	}
	if p.Type != "block_actions" || len(p.Actions) == 0 {
		return Interactivity{}, false
	}
	act := p.Actions[0]
	if strings.TrimSpace(act.ActionID) == "" {
		return Interactivity{}, false
	}
	return Interactivity{
		Type:        p.Type,
		TeamID:      p.Team.ID,
		ChannelID:   p.Channel.ID,
		MessageTS:   p.Message.TS,
		UserID:      p.User.ID,
		ActionID:    act.ActionID,
		ActionValue: act.Value,
	}, true
}
