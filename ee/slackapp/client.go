// ee/slackapp/client.go
// SPDX-License-Identifier: LicenseRef-Kiwi-BSL-1.1
// Copyright (c) 2026 RunKiwi. Licensed under the Business Source License 1.1.
// See ee/LICENSE. This is Control Plane code and is NOT Apache-2.0.

package slackapp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Client calls the Slack Web API. Every call carries the caller-supplied
// bot token explicitly rather than the Client holding one — a single Client
// serves every installed workspace, the same way ee/githubapp's Client mints
// tokens per-installation rather than being built for one.
type Client struct {
	http    *http.Client
	baseURL string
}

type Option func(*Client)

func WithBaseURL(u string) Option          { return func(c *Client) { c.baseURL = strings.TrimRight(u, "/") } }
func WithHTTPClient(h *http.Client) Option { return func(c *Client) { c.http = h } }

func New(opts ...Option) *Client {
	c := &Client{http: &http.Client{Timeout: 15 * time.Second}, baseURL: "https://slack.com/api"}
	for _, fn := range opts {
		fn(c)
	}
	return c
}

type slackEnvelope struct {
	OK    bool   `json:"ok"`
	Error string `json:"error"`
	TS    string `json:"ts"`
}

func (c *Client) post(ctx context.Context, token, method string, body map[string]interface{}) (slackEnvelope, error) {
	raw, err := json.Marshal(body)
	if err != nil {
		return slackEnvelope{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/"+method, bytes.NewReader(raw))
	if err != nil {
		return slackEnvelope{}, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return slackEnvelope{}, fmt.Errorf("slackapp: %s: %w", method, err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))

	var env slackEnvelope
	if err := json.Unmarshal(respBody, &env); err != nil {
		return slackEnvelope{}, fmt.Errorf("slackapp: %s: decode response: %w", method, err)
	}
	if !env.OK {
		return env, fmt.Errorf("slackapp: %s: %s", method, env.Error)
	}
	return env, nil
}

// PostMessage sends a new message, optionally into an existing thread
// (threadTS empty means a fresh top-level message). Returns the new
// message's own ts, which callers persist as the editable status message id.
func (c *Client) PostMessage(ctx context.Context, token, channel, threadTS, text string) (string, error) {
	body := map[string]interface{}{"channel": channel, "text": text}
	if threadTS != "" {
		body["thread_ts"] = threadTS
	}
	env, err := c.post(ctx, token, "chat.postMessage", body)
	if err != nil {
		return "", err
	}
	return env.TS, nil
}

// EditMessage overwrites an existing message's text — how the status
// message moves through created -> running -> done without spamming the
// channel with a new message per transition.
func (c *Client) EditMessage(ctx context.Context, token, channel, ts, text string) error {
	_, err := c.post(ctx, token, "chat.update", map[string]interface{}{"channel": channel, "ts": ts, "text": text})
	return err
}

func (c *Client) AddReaction(ctx context.Context, token, channel, ts, emoji string) error {
	_, err := c.post(ctx, token, "reactions.add", map[string]interface{}{"channel": channel, "timestamp": ts, "name": emoji})
	return err
}

func (c *Client) RemoveReaction(ctx context.Context, token, channel, ts, emoji string) error {
	_, err := c.post(ctx, token, "reactions.remove", map[string]interface{}{"channel": channel, "timestamp": ts, "name": emoji})
	// already_reacted / no_reaction races are not failures worth surfacing —
	// the emoji swap is a best-effort UI touch, same posture as the GitHub
	// comment-trigger's acknowledge().
	if err != nil && strings.Contains(err.Error(), "no_reaction") {
		return nil
	}
	return err
}

// Message is one line of channel/thread history, reduced to what context
// assembly (Task 8) needs.
type Message struct {
	UserID string `json:"user"`
	Text   string `json:"text"`
	TS     string `json:"ts"`
}

// historyPage fetches one page and returns Slack's own cursor for the next
// one — empty when there isn't one, per Slack's response_metadata.
func (c *Client) historyPage(ctx context.Context, token, method, channel string, extra url.Values) (msgs []Message, nextCursor string, err error) {
	q := url.Values{"channel": {channel}}
	for k, v := range extra {
		q[k] = v
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/"+method+"?"+q.Encode(), nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, "", fmt.Errorf("slackapp: %s: %w", method, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))

	var out struct {
		OK               bool      `json:"ok"`
		Error            string    `json:"error"`
		Messages         []Message `json:"messages"`
		ResponseMetadata struct {
			NextCursor string `json:"next_cursor"`
		} `json:"response_metadata"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, "", fmt.Errorf("slackapp: %s: decode: %w", method, err)
	}
	if !out.OK {
		return nil, "", fmt.Errorf("slackapp: %s: %s", method, out.Error)
	}
	return out.Messages, out.ResponseMetadata.NextCursor, nil
}

func (c *Client) history(ctx context.Context, token, method, channel string, extra url.Values) ([]Message, error) {
	msgs, _, err := c.historyPage(ctx, token, method, channel, extra)
	return msgs, err
}

// ConversationHistory returns channel messages, newest first, capped at
// limit — the fixed-lookback half of context assembly (Task 8). Never
// paginates: limit is always well under a page (fixedLookback/
// escalatedLookback in ee/orchestrator/slack_context.go, both far below
// Slack's own page size), so there is never a second page to follow.
func (c *Client) ConversationHistory(ctx context.Context, token, channel string, limit int) ([]Message, error) {
	return c.history(ctx, token, "conversations.history", channel, url.Values{"limit": {fmt.Sprintf("%d", limit)}})
}

// maxReplyPages bounds how many pages ConversationReplies will follow for
// one thread — a tuning knob, not a contract, the same posture
// fixedLookback/escalatedLookback take. Without a cap, a pathological
// thread would make this call cost proportional to the whole thread's
// length rather than to the summary the caller actually wants; with one, a
// very long thread degrades to "the first maxReplyPages worth" instead of
// hanging or ballooning the request.
const maxReplyPages = 10

// ConversationReplies returns a whole thread, oldest first, following
// Slack's cursor across pages up to maxReplyPages. Before this it fetched
// only the first page — Slack does not document conversations.replies'
// default page size as unbounded, so a long-running thread silently
// truncated with no error and no sign anything was cut off.
func (c *Client) ConversationReplies(ctx context.Context, token, channel, threadTS string) ([]Message, error) {
	var all []Message
	cursor := ""
	for page := 0; page < maxReplyPages; page++ {
		extra := url.Values{"ts": {threadTS}}
		if cursor != "" {
			extra["cursor"] = []string{cursor}
		}
		msgs, next, err := c.historyPage(ctx, token, "conversations.replies", channel, extra)
		if err != nil {
			return nil, err
		}
		all = append(all, msgs...)
		if next == "" {
			break
		}
		cursor = next
	}
	return all, nil
}

// Button is one option in the continue/fork/new disambiguation prompt.
type Button struct {
	Label    string
	ActionID string
	Value    string
}

// PostInteractiveButtons posts a Block Kit message with one button per
// Button, for the ambiguous thread-reply case (Task 11).
func (c *Client) PostInteractiveButtons(ctx context.Context, token, channel, threadTS, text string, buttons []Button) (string, error) {
	var elements []map[string]interface{}
	for _, b := range buttons {
		elements = append(elements, map[string]interface{}{
			"type":      "button",
			"text":      map[string]interface{}{"type": "plain_text", "text": b.Label},
			"action_id": b.ActionID,
			"value":     b.Value,
		})
	}
	blocks := []map[string]interface{}{
		{"type": "section", "text": map[string]interface{}{"type": "mrkdwn", "text": text}},
		{"type": "actions", "elements": elements},
	}
	env, err := c.post(ctx, token, "chat.postMessage", map[string]interface{}{
		"channel": channel, "thread_ts": threadTS, "text": text, "blocks": blocks,
	})
	if err != nil {
		return "", err
	}
	return env.TS, nil
}

// OAuthResult is what Slack's oauth.v2.access returns on a successful
// install: the bot token to store, and which team it belongs to.
type OAuthResult struct {
	AccessToken string
	TeamID      string
	TeamName    string
}

var ErrOAuthExchangeFailed = errors.New("slackapp: oauth exchange failed")

// ExchangeOAuthCode trades the "Add to Slack" redirect's code for a bot
// token — the Slack half of the install flow Task 4 wires end to end.
func (c *Client) ExchangeOAuthCode(ctx context.Context, clientID, clientSecret, code, redirectURI string) (OAuthResult, error) {
	form := url.Values{
		"client_id":     {clientID},
		"client_secret": {clientSecret},
		"code":          {code},
		"redirect_uri":  {redirectURI},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/oauth.v2.access", strings.NewReader(form.Encode()))
	if err != nil {
		return OAuthResult{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.http.Do(req)
	if err != nil {
		return OAuthResult{}, fmt.Errorf("slackapp: oauth exchange: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))

	var out struct {
		OK          bool   `json:"ok"`
		Error       string `json:"error"`
		AccessToken string `json:"access_token"`
		Team        struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"team"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return OAuthResult{}, fmt.Errorf("slackapp: oauth exchange: decode: %w", err)
	}
	if !out.OK || out.AccessToken == "" {
		return OAuthResult{}, fmt.Errorf("%w: %s", ErrOAuthExchangeFailed, out.Error)
	}
	return OAuthResult{AccessToken: out.AccessToken, TeamID: out.Team.ID, TeamName: out.Team.Name}, nil
}
