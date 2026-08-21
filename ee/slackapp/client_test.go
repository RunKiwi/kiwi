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
