// SPDX-License-Identifier: LicenseRef-Kiwi-BSL-1.1
// Copyright (c) 2026 RunKiwi. Licensed under the Business Source License 1.1.
// See ee/LICENSE. This is Control Plane code and is NOT Apache-2.0.

package planner

import (
	"context"
	"strings"
	"testing"

	"github.com/ibreakthecloud/kiwi/pkg/provider"
	"github.com/ibreakthecloud/kiwi/pkg/store"
)

// usageFake is a Completer that also reports usage, standing in for a real
// provider so cost aggregation can be exercised without a network call.
type usageFake struct {
	in, out int64
	cost    float64
}

func (f *usageFake) Complete(ctx context.Context, system, user string) (string, error) {
	return `{"summary":"s","workers":[{"id":"w1","task":"t","file":"f","model":"m"}]}`, nil
}

func (f *usageFake) Usage() (int64, int64, float64) { return f.in, f.out, f.cost }

// The provider name written into the manifest — and from there into the signed
// execution record — must match the identifier used everywhere else: the
// credential name, the integrations catalog, and the dashboard. "google" is not
// that identifier.
func TestProviderNamingIsCanonical(t *testing.T) {
	if provider.ProviderOf("gemini-flash-latest") != "gemini" {
		t.Errorf("gemini model should map to %q, got %q", "gemini", provider.ProviderOf("gemini-flash-latest"))
	}
	if provider.ProviderOf("claude-opus-4-8") != "anthropic" {
		t.Errorf("claude model should map to anthropic, got %q", provider.ProviderOf("claude-opus-4-8"))
	}
	if provider.ProviderOf("gpt-5-mini") != "openai" {
		t.Errorf("gpt model should map to %q, got %q", "openai", provider.ProviderOf("gpt-5-mini"))
	}
	// An unknown id falls back to Anthropic, matching ModelCostUSD's pricing
	// fallback so naming and billing never disagree about the same model.
	if provider.ProviderOf("some-new-model") != "anthropic" {
		t.Errorf("unknown model should fall back to anthropic, got %q", provider.ProviderOf("some-new-model"))
	}
}

// Admission resolves the org's key by the ARCHITECT's provider. If it looked up
// the wrong credential name, an org that connected only OpenAI would be told it
// has no key at all while its key sat in the store unread.
//
// This used to be a property of Control-Plane planning. Planning moved into the
// daemon, but the check did not go away — it moved to the model the Control
// Plane still chooses.
func TestAdmissionResolvesTheKeyForTheArchitectsProvider(t *testing.T) {
	s := newTestStore(t)
	svc := NewService(s, nil, nil)
	seedOrg(t, s, "org-1")

	if err := s.SaveCredential(context.Background(), "org-1", "OPENAI_API_KEY", store.CredentialLLM, "sk-openai-x"); err != nil {
		t.Fatal(err)
	}

	// An OpenAI Architect over an OpenAI Implementer must find OPENAI_API_KEY.
	if _, err := svc.SubmitPlan(context.Background(), PlanRequest{
		OrgID: "org-1", Task: "do a thing",
		Model: "gpt-5-mini", ArchitectModel: "gpt-5",
	}); err != nil {
		t.Fatalf("an OpenAI task with an OpenAI key connected: %v", err)
	}

	// The complement: an Anthropic Architect with only an OpenAI key connected
	// must fail naming Anthropic, not OpenAI — otherwise the user adds the key
	// they already have.
	_, err := svc.SubmitPlan(context.Background(), PlanRequest{
		OrgID: "org-1", Task: "do a thing",
		Model: "gpt-5-mini", ArchitectModel: "claude-opus-4-8",
	})
	if err == nil {
		t.Fatal("expected an error: no Anthropic key is connected")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "anthropic") {
		t.Errorf("error should name the missing provider (anthropic), got %q", err)
	}
}

// An org with no usable key must be told so in terms the dashboard's error
// mapper recognises, so the user gets the link to Integrations rather than a
// bare string.
func TestPlanningWithoutAKeyIsAnActionableError(t *testing.T) {
	s := newTestStore(t)
	svc := NewService(s, nil, nil)
	seedOrg(t, s, "org-1")

	t.Setenv("KIWI_PLANNER", "llm")
	t.Setenv("KIWI_PLANNER_API_KEY", "")

	_, err := svc.SubmitPlan(context.Background(), PlanRequest{
		OrgID: "org-1",
		Task:  "do a thing",
	})
	if err == nil {
		t.Fatal("expected an error when the org has no provider key")
	}
	msg := strings.ToLower(err.Error())
	// parseActionableError in the dashboard keys off this phrase.
	if !strings.Contains(msg, "provider key") {
		t.Errorf("error must name a provider key so the UI can offer Integrations, got %q", err)
	}
	if !strings.Contains(msg, "anthropic") {
		t.Errorf("error should name the provider it needed, got %q", err)
	}
}
