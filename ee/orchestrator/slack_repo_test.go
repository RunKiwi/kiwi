// SPDX-License-Identifier: LicenseRef-Kiwi-BSL-1.1
// Copyright (c) 2026 RunKiwi. Licensed under the Business Source License 1.1.
// See ee/LICENSE. This is Control Plane code and is NOT Apache-2.0.

package orchestrator

import (
	"context"
	"strings"
	"testing"
)

func TestInlineRepoOverrideWinsRegardlessOfBinding(t *testing.T) {
	got, ok := inlineRepoOverride("fix the bug in repo:acme/widget please")
	if !ok || got != "acme/widget" {
		t.Fatalf("got %q, ok=%v", got, ok)
	}
}

func TestInlineRepoOverrideAbsentReturnsFalse(t *testing.T) {
	if _, ok := inlineRepoOverride("fix the login bug"); ok {
		t.Fatal("expected no override to be found")
	}
}

func TestInferRepoPicksTheClearWinner(t *testing.T) {
	repos := []string{"auth-service", "billing-service", "docs-site"}
	complete := func(ctx context.Context, system, user string) (string, error) {
		return `{"repo": "auth-service", "confidence": "high"}`, nil
	}
	got, ambiguous, err := inferRepo(context.Background(), complete, repos, "fix the login bug")
	if err != nil {
		t.Fatalf("inferRepo: %v", err)
	}
	if ambiguous || got != "auth-service" {
		t.Fatalf("got %q ambiguous=%v", got, ambiguous)
	}
}

func TestInferRepoReportsAmbiguousOnLowConfidence(t *testing.T) {
	repos := []string{"api-v1", "api-v2"}
	complete := func(ctx context.Context, system, user string) (string, error) {
		return `{"repo": "", "confidence": "low", "candidates": ["api-v1", "api-v2"]}`, nil
	}
	_, ambiguous, err := inferRepo(context.Background(), complete, repos, "fix the bug")
	if err != nil {
		t.Fatalf("inferRepo: %v", err)
	}
	if !ambiguous {
		t.Fatal("expected ambiguous=true on low confidence")
	}
}

// Regression test for the incident where a user named the repo in one
// message ("docs repo is runkiwi/docs on github") and then, in a later
// message in the same thread, only said "work on this" — resolveSlackRepo
// passed the bare current message to inferRepo instead of the context-
// assembled instruction fetchSlackContext already builds from thread
// history, so the repo the user had already stated was invisible to the
// model and every such follow-up came back ambiguous.
func TestPickRepoFromCandidatesSeesThreadHistoryInInstructionNotBareMention(t *testing.T) {
	names := []string{"runkiwi/docs", "runkiwi/kiwi"}
	nameToURL := map[string]string{
		"runkiwi/docs": "https://github.com/runkiwi/docs",
		"runkiwi/kiwi": "https://github.com/runkiwi/kiwi",
	}
	var gotUser string
	complete := func(ctx context.Context, system, user string) (string, error) {
		gotUser = user
		if strings.Contains(user, "docs repo is runkiwi/docs") {
			return `{"repo": "runkiwi/docs", "confidence": "high"}`, nil
		}
		return `{"repo": "", "confidence": "low"}`, nil
	}
	instruction := "Context from the conversation:\nU1: docs repo is runkiwi/docs on github\n\nInstruction: work on this"

	repoURL, ambiguousReply := pickRepoFromCandidates(context.Background(), complete, names, nameToURL, instruction)
	if ambiguousReply != "" {
		t.Fatalf("got ambiguousReply %q, want the repo named earlier in the thread to resolve it", ambiguousReply)
	}
	if repoURL != "https://github.com/runkiwi/docs" {
		t.Fatalf("got repoURL %q, want runkiwi/docs", repoURL)
	}
	if !strings.Contains(gotUser, "docs repo is runkiwi/docs") {
		t.Fatalf("completer prompt %q did not include the thread history", gotUser)
	}
}

// The bare mention alone, with no thread history folded in, must still
// report ambiguous rather than guess — this is what the fix above must NOT
// regress: it's the completer input that changed (bare text vs.
// context-assembled instruction), not the "only high confidence auto-picks"
// rule itself.
func TestPickRepoFromCandidatesStillAmbiguousWithNoRepoSignalAtAll(t *testing.T) {
	names := []string{"runkiwi/docs", "runkiwi/kiwi"}
	nameToURL := map[string]string{"runkiwi/docs": "https://github.com/runkiwi/docs"}
	complete := func(ctx context.Context, system, user string) (string, error) {
		return `{"repo": "", "confidence": "low"}`, nil
	}
	repoURL, ambiguousReply := pickRepoFromCandidates(context.Background(), complete, names, nameToURL, "work on this")
	if ambiguousReply == "" || repoURL != "" {
		t.Fatalf("got repoURL=%q ambiguousReply=%q, want ambiguous with no repo resolved", repoURL, ambiguousReply)
	}
}
