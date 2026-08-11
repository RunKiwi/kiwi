// SPDX-License-Identifier: LicenseRef-Kiwi-BSL-1.1
// Copyright (c) 2026 RunKiwi. Licensed under the Business Source License 1.1.
// See ee/LICENSE. This is Control Plane code and is NOT Apache-2.0.

package orchestrator

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCollaboratorPermissionReadsTheAnswer(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/acme/widgets/collaborators/reviewer/permission" {
			t.Errorf("path = %s", r.URL.Path)
		}
		if auth := r.Header.Get("Authorization"); auth != "Bearer ghs-token" {
			t.Errorf("authorization = %q", auth)
		}
		_, _ = w.Write([]byte(`{"permission":"write"}`))
	}))
	defer srv.Close()

	got, err := collaboratorPermission(context.Background(), srv.URL, "ghs-token", "acme", "widgets", "reviewer")
	if err != nil {
		t.Fatal(err)
	}
	if got != "write" {
		t.Errorf("permission = %q, want write", got)
	}
}

// A failed call must not be indistinguishable from "no access". Returning ""
// with no error would read as a refusal, and a GitHub blip would silently stop
// a team's reviews working with nothing in the logs to explain it.
func TestCollaboratorPermissionReportsFailures(t *testing.T) {
	for _, status := range []int{http.StatusNotFound, http.StatusUnauthorized, http.StatusInternalServerError} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(status)
		}))
		got, err := collaboratorPermission(context.Background(), srv.URL, "t", "acme", "widgets", "stranger")
		srv.Close()
		if err == nil {
			t.Errorf("status %d: expected an error, got permission %q", status, got)
		}
	}
}

// The two comment kinds live at different endpoints. Reacting to a review
// comment on the issue-comment path silently reacts to the wrong thing.
func TestAddReactionUsesThePathForTheCommentKind(t *testing.T) {
	for _, tc := range []struct{ event, wantPath string }{
		{"issue_comment", "/repos/acme/widgets/issues/comments/555/reactions"},
		{"pull_request_review_comment", "/repos/acme/widgets/pulls/comments/555/reactions"},
	} {
		var gotPath, gotBody string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotPath = r.URL.Path
			buf := make([]byte, r.ContentLength)
			_, _ = r.Body.Read(buf)
			gotBody = string(buf)
			w.WriteHeader(http.StatusCreated)
		}))
		err := addReaction(context.Background(), srv.URL, "t", "acme", "widgets", tc.event, 555, "eyes")
		srv.Close()
		if err != nil {
			t.Fatalf("%s: %v", tc.event, err)
		}
		if gotPath != tc.wantPath {
			t.Errorf("%s: path = %s, want %s", tc.event, gotPath, tc.wantPath)
		}
		if !strings.Contains(gotBody, "eyes") {
			t.Errorf("%s: body = %s, want the reaction content", tc.event, gotBody)
		}
	}
}

// A review body has no reaction endpoint of its own, so there is nothing to
// react to and pretending otherwise would post to a comment id that is really
// a review id.
func TestAddReactionSkipsReviewBodies(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	if err := addReaction(context.Background(), srv.URL, "t", "acme", "widgets", "pull_request_review", 888, "eyes"); err != nil {
		t.Fatalf("a review body should be skipped quietly, got %v", err)
	}
	if called {
		t.Error("no request should have been made for a review body")
	}
}

func TestCreateIssueCommentPostsToThePullRequestThread(t *testing.T) {
	var gotPath, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		buf := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(buf)
		gotBody = string(buf)
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	if err := createIssueComment(context.Background(), srv.URL, "t", "acme", "widgets", 42, "on it"); err != nil {
		t.Fatal(err)
	}
	if gotPath != "/repos/acme/widgets/issues/42/comments" {
		t.Errorf("path = %s", gotPath)
	}
	if !strings.Contains(gotBody, "on it") {
		t.Errorf("body = %s", gotBody)
	}
}
