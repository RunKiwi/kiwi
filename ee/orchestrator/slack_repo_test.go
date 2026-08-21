// SPDX-License-Identifier: LicenseRef-Kiwi-BSL-1.1
// Copyright (c) 2026 RunKiwi. Licensed under the Business Source License 1.1.
// See ee/LICENSE. This is Control Plane code and is NOT Apache-2.0.

package orchestrator

import (
	"context"
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
