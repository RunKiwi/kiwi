// SPDX-License-Identifier: LicenseRef-Kiwi-BSL-1.1
// Copyright (c) 2026 RunKiwi. Licensed under the Business Source License 1.1.
// See ee/LICENSE. This is Control Plane code and is NOT Apache-2.0.

package slackapp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPostMessageReturnsTS(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat.postMessage" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"ok": true, "ts": "100.001"})
	}))
	defer srv.Close()

	c := New(WithBaseURL(srv.URL))
	ts, err := c.PostMessage(t.Context(), "xoxb-test", "C1", "", "hello")
	if err != nil {
		t.Fatalf("PostMessage: %v", err)
	}
	if ts != "100.001" {
		t.Fatalf("got ts %q", ts)
	}
}

func TestPostMessageReturnsErrorOnSlackNotOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": "channel_not_found"})
	}))
	defer srv.Close()

	c := New(WithBaseURL(srv.URL))
	if _, err := c.PostMessage(t.Context(), "xoxb-test", "C1", "", "hello"); err == nil {
		t.Fatal("expected an error when Slack reports ok=false")
	}
}

// Regression test for the missing pagination: before this, ConversationReplies
// fetched only the first page, so a thread longer than one page silently
// truncated with no error and no sign anything was cut off.
func TestConversationRepliesFollowsCursorAcrossPages(t *testing.T) {
	var gotCursors []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/conversations.replies" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		cursor := r.URL.Query().Get("cursor")
		gotCursors = append(gotCursors, cursor)
		w.Header().Set("Content-Type", "application/json")
		switch cursor {
		case "":
			json.NewEncoder(w).Encode(map[string]any{
				"ok":                true,
				"messages":          []map[string]any{{"user": "U1", "text": "first page", "ts": "1"}},
				"response_metadata": map[string]any{"next_cursor": "page2"},
			})
		case "page2":
			json.NewEncoder(w).Encode(map[string]any{
				"ok":                true,
				"messages":          []map[string]any{{"user": "U1", "text": "second page", "ts": "2"}},
				"response_metadata": map[string]any{"next_cursor": ""},
			})
		default:
			t.Fatalf("unexpected cursor %q", cursor)
		}
	}))
	defer srv.Close()

	c := New(WithBaseURL(srv.URL))
	msgs, err := c.ConversationReplies(t.Context(), "xoxb-test", "C1", "100.001")
	if err != nil {
		t.Fatalf("ConversationReplies: %v", err)
	}
	if len(gotCursors) != 2 {
		t.Fatalf("expected exactly 2 page requests, got %d: %v", len(gotCursors), gotCursors)
	}
	if len(msgs) != 2 || msgs[0].Text != "first page" || msgs[1].Text != "second page" {
		t.Fatalf("expected messages from both pages, got %+v", msgs)
	}
}

// The page cap must actually stop the loop, not just exist as a constant —
// a thread that never runs out of next_cursor must not fetch forever.
func TestConversationRepliesStopsAtThePageCap(t *testing.T) {
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"ok":                true,
			"messages":          []map[string]any{{"user": "U1", "text": "msg", "ts": "1"}},
			"response_metadata": map[string]any{"next_cursor": "always-more"},
		})
	}))
	defer srv.Close()

	c := New(WithBaseURL(srv.URL))
	msgs, err := c.ConversationReplies(t.Context(), "xoxb-test", "C1", "100.001")
	if err != nil {
		t.Fatalf("ConversationReplies: %v", err)
	}
	if requests != maxReplyPages {
		t.Fatalf("expected exactly maxReplyPages (%d) requests against a never-ending cursor, got %d", maxReplyPages, requests)
	}
	if len(msgs) != maxReplyPages {
		t.Fatalf("expected %d messages (one per page), got %d", maxReplyPages, len(msgs))
	}
}

func TestAddReactionPostsExpectedFields(t *testing.T) {
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))
	defer srv.Close()

	c := New(WithBaseURL(srv.URL))
	if err := c.AddReaction(t.Context(), "xoxb-test", "C1", "100.001", "eyes"); err != nil {
		t.Fatalf("AddReaction: %v", err)
	}
	if gotBody["channel"] != "C1" || gotBody["timestamp"] != "100.001" || gotBody["name"] != "eyes" {
		t.Fatalf("got body %+v", gotBody)
	}
}
