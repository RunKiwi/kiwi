// SPDX-License-Identifier: LicenseRef-Kiwi-BSL-1.1
// Copyright (c) 2026 RunKiwi. Licensed under the Business Source License 1.1.
// See ee/LICENSE. This is Control Plane code and is NOT Apache-2.0.

package orchestrator

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ibreakthecloud/kiwi/pkg/store"
)

func f64(v float64) *float64 { return &v }
func iPtr(v int) *int        { return &v }
func bPtr(v bool) *bool      { return &v }

func seedSelectableOpenRouterModel(t *testing.T, s *Server, modelID, tier string, outputCostPerM float64) {
	t.Helper()
	now := time.Now().UTC()
	if err := s.storage.UpsertCatalogModel(context.Background(), &store.CatalogModel{
		OrgID: store.GlobalCatalogOrg, ModelID: modelID, Provider: "openrouter",
		InputCostPerM: f64(0.10), OutputCostPerM: f64(outputCostPerM),
		ContextLength: iPtr(128000), SupportsTools: bPtr(true), Modality: "text->text",
		Tier: tier, KiwiProvided: true, Selectable: true,
		// Well past CheapestKiwiFundedModel's catalogMaturityWindow
		// (pkg/store/model_catalog.go) — these tests are about tier/provider
		// selection, not catalog freshness, so a just-seeded FirstSeenAt would
		// fail the freshness gate for a reason unrelated to what's under test.
		Source: "discovered", FirstSeenAt: now.Add(-30 * 24 * time.Hour), LastSeenAt: now,
	}); err != nil {
		t.Fatalf("seed catalog model: %v", err)
	}
}

// Before this, slackCompleter always required a Gemini platform key — an
// OpenRouter-only deployment (the platform's actual configuration) could
// never run Slack's inference calls at all.
func TestSlackCompleterSucceedsOnOpenRouterAloneWhenCatalogHasAModel(t *testing.T) {
	s := newTestServer(t)
	t.Setenv("KIWI_PLATFORM_OPENROUTER_API_KEY", "sk-or-platform")
	// Deliberately no KIWI_PLATFORM_GEMINI_API_KEY: proves the catalog path
	// does not fall through to (or require) Gemini.
	seedSelectableOpenRouterModel(t, s, "cheap-router-model", store.TierFrontier, 0.50)

	complete, err := s.slackCompleter(context.Background())
	if err != nil {
		t.Fatalf("slackCompleter: %v", err)
	}
	if complete == nil {
		t.Fatal("expected a non-nil completeFunc")
	}
}

// The cheapest qualifying candidate is chosen, not merely a qualifying one —
// same guarantee pkg/store's TestCheapestKiwiFundedModelPicksLowestOutputCost
// makes at the store layer.
func TestSlackCompleterPicksTheCheapestCatalogCandidate(t *testing.T) {
	s := newTestServer(t)
	t.Setenv("KIWI_PLATFORM_OPENROUTER_API_KEY", "sk-or-platform")
	seedSelectableOpenRouterModel(t, s, "pricier-model", store.TierEconomy, 1.80)
	seedSelectableOpenRouterModel(t, s, "cheapest-model", store.TierEconomy, 0.20)

	got, ok, err := s.storage.CheapestKiwiFundedModel(context.Background(), store.GlobalCatalogOrg, "openrouter", store.TierEconomy)
	if err != nil || !ok {
		t.Fatalf("CheapestKiwiFundedModel: ok=%v err=%v", ok, err)
	}
	if got != "cheapest-model" {
		t.Fatalf("got %q, want the cheaper of the two candidates", got)
	}
}

// No OpenRouter platform key, no catalog model, and no Gemini key: every
// path is exhausted and slackCompleter must report the failure rather than
// return a completeFunc that would panic or call an unconfigured provider.
func TestSlackCompleterFailsWhenNothingIsConfigured(t *testing.T) {
	s := newTestServer(t)
	if _, err := s.slackCompleter(context.Background()); err == nil {
		t.Fatal("expected an error with no platform key configured anywhere")
	}
}

// A qualifying catalog model exists but Kiwi holds no OpenRouter key to pay
// for it (platform_keys.go's own rule): slackCompleter must fall through to
// Gemini rather than trying to build a provider with no key.
func TestSlackCompleterFallsBackToGeminiWithoutAnOpenRouterKey(t *testing.T) {
	s := newTestServer(t)
	t.Setenv("KIWI_PLATFORM_GEMINI_API_KEY", "sk-gemini-platform")
	seedSelectableOpenRouterModel(t, s, "cheap-router-model", store.TierFrontier, 0.50)

	complete, err := s.slackCompleter(context.Background())
	if err != nil {
		t.Fatalf("slackCompleter: %v", err)
	}
	if complete == nil {
		t.Fatal("expected a non-nil completeFunc from the Gemini fallback")
	}
}

// Regression test for the incident where a slow frontier reasoning model
// (deepseek-r1-class) ran past handleSlackTrigger's own 60s budget with no
// per-call bound at all, leaving the user with total silence. boundCompleter
// must give every call its own deadline rather than trusting the caller's.
func TestBoundCompleterCapsEachCallWithItsOwnDeadline(t *testing.T) {
	var gotDeadline time.Time
	var gotOK bool
	inner := func(ctx context.Context, system, user string) (string, error) {
		gotDeadline, gotOK = ctx.Deadline()
		return "ok", nil
	}

	wrapped := boundCompleter(inner)
	before := time.Now()
	if _, err := wrapped(context.Background(), "sys", "user"); err != nil {
		t.Fatalf("wrapped completer: %v", err)
	}
	after := time.Now()

	if !gotOK {
		t.Fatal("inner call received a context with no deadline at all")
	}
	if gotDeadline.Before(before.Add(slackCompleterTimeout-time.Second)) || gotDeadline.After(after.Add(slackCompleterTimeout+time.Second)) {
		t.Errorf("deadline %v is not ~%v out from the call, got window [%v, %v]", gotDeadline, slackCompleterTimeout, before, after)
	}
}

// Regression test: posting a reply on a context that already expired
// upstream (fetchSlackContext/resolveSlackRepo's completer calls exhausting
// handleSlackTrigger's 60s budget) fails PostMessage silently, since its
// error return isn't otherwise surfaced anywhere a human sees it — the exact
// mechanism behind the incident where a Slack trigger produced zero visible
// response, not even the "not sure which repository" fallback.
func TestReplyCtxSubstitutesAFreshContextOnlyWhenTheOriginalIsDead(t *testing.T) {
	live := context.Background()
	got, cancel := replyCtx(live)
	cancel()
	if got != live {
		t.Error("replyCtx replaced a still-live context; it must pass it through unchanged")
	}

	dead, dcancel := context.WithCancel(context.Background())
	dcancel()
	got, cancel = replyCtx(dead)
	defer cancel()
	if got == dead {
		t.Fatal("replyCtx passed through an already-cancelled context instead of substituting one")
	}
	if got.Err() != nil {
		t.Error("the substitute context is itself already done")
	}
	if _, ok := got.Deadline(); !ok {
		t.Error("the substitute context has no deadline of its own")
	}
}

func TestAssembleSlackContextUsesFixedLookbackWhenSufficient(t *testing.T) {
	history := []string{"U1: the login page 500s on bad passwords", "U2: seeing it in prod too"}
	complete := func(ctx context.Context, system, user string) (string, error) {
		if strings.Contains(user, "500s on bad passwords") {
			return `{"sufficient": true}`, nil
		}
		t.Fatalf("unexpected prompt: %s", user)
		return "", nil
	}
	got, err := assembleContext(context.Background(), complete, history, "fix this bug")
	if err != nil {
		t.Fatalf("assembleContext: %v", err)
	}
	if !strings.Contains(got, "500s on bad passwords") || !strings.Contains(got, "fix this bug") {
		t.Fatalf("got %q", got)
	}
}

func TestAssembleSlackContextEscalatesWhenInsufficient(t *testing.T) {
	calls := 0
	complete := func(ctx context.Context, system, user string) (string, error) {
		calls++
		if calls == 1 {
			return `{"sufficient": false}`, nil
		}
		return `{"sufficient": true}`, nil
	}
	history := []string{"U1: something's wrong"}
	escalated := []string{"U1: something's wrong", "U2: it's the login flow, 500 on bad password"}
	got, err := assembleContextEscalating(context.Background(), complete, history, escalated, "fix this")
	if err != nil {
		t.Fatalf("assembleContextEscalating: %v", err)
	}
	if calls != 2 {
		t.Fatalf("expected exactly one escalation call, got %d total calls", calls)
	}
	if !strings.Contains(got, "500 on bad password") {
		t.Fatalf("expected the escalated history in the result, got %q", got)
	}
}
