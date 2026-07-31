package provider

import "testing"

// ProviderOf is the single routing rule in the Go tree — the daemon picks a key
// with it, the planner picks a key with it, and the signed execution record
// names a provider with it. A model that routes one way for the key and another
// way for the record is an attestation that does not match what ran.
func TestProviderOfRoutesEachFamily(t *testing.T) {
	cases := map[string]string{
		"claude-opus-4-8":           ProviderAnthropic,
		"claude-haiku-4-5-20251001": ProviderAnthropic,
		"gemini-flash-latest":       ProviderGemini,
		"gemini-2.0-flash":          ProviderGemini,
		"gpt-5":                     ProviderOpenAI,
		"gpt-5-mini":                ProviderOpenAI,
		"gpt-4.1":                   ProviderOpenAI,
		"gpt-4o-mini":               ProviderOpenAI,
		"GPT-5":                     ProviderOpenAI,
		"o3-mini":                   ProviderOpenAI,
		"o1-preview":                ProviderOpenAI,
		"chatgpt-4o-latest":         ProviderOpenAI,
		// Unknown ids fall back to Anthropic, matching ModelCostUSD's pricing
		// fallback so naming and billing never disagree about the same model.
		"some-new-model": ProviderAnthropic,
		"":               ProviderAnthropic,
	}
	for model, want := range cases {
		if got := ProviderOf(model); got != want {
			t.Errorf("ProviderOf(%q) = %q, want %q", model, got, want)
		}
	}
}

// The prefix list must not be greedy. "opus-*" beginning with an "o" is the
// mistake a bare single-letter prefix would make, and it would route an
// Anthropic model to a provider whose key the org may never have connected.
func TestProviderOfDoesNotOverMatchOnO(t *testing.T) {
	for _, m := range []string{"opus-4-8", "orca-mini", "olmo-2"} {
		if got := ProviderOf(m); got == ProviderOpenAI {
			t.Errorf("ProviderOf(%q) = openai; the o-prefixes must stay narrow", m)
		}
	}
}

// Every provider must resolve to a distinct credential name. Two providers
// sharing one name would mean connecting one key silently satisfies the other,
// and the task would fail at the API with someone else's key.
func TestCredentialNamesAreDistinct(t *testing.T) {
	seen := map[string]string{}
	for _, p := range []string{ProviderAnthropic, ProviderGemini, ProviderOpenAI} {
		name := CredentialNameFor(p)
		if name == "" {
			t.Errorf("provider %q has no credential name", p)
		}
		if other, dup := seen[name]; dup {
			t.Errorf("providers %q and %q both resolve to %q", p, other, name)
		}
		seen[name] = p
	}
	if got := CredentialNameFor(ProviderOpenAI); got != "OPENAI_API_KEY" {
		t.Errorf("OpenAI credential name = %q, want OPENAI_API_KEY", got)
	}
}

func TestDisplayNames(t *testing.T) {
	cases := map[string]string{
		ProviderAnthropic: "Anthropic",
		ProviderGemini:    "Gemini",
		ProviderOpenAI:    "OpenAI",
	}
	for p, want := range cases {
		if got := DisplayNameFor(p); got != want {
			t.Errorf("DisplayNameFor(%q) = %q, want %q", p, got, want)
		}
	}
}

// Every model the dashboard offers must be priced explicitly. A missing entry
// falls back to a family default, which quietly approximates the per-job budget
// cap and the Spend page for a model people actually run.
func TestRecommendedModelsArePriced(t *testing.T) {
	for _, m := range []string{
		"claude-opus-4-8", "claude-sonnet-5", "claude-haiku-4-5-20251001",
		"gemini-2.0-flash", "gemini-flash-latest",
		"gpt-5", "gpt-5-mini", "gpt-4.1-mini",
	} {
		if _, ok := PricingMap[m]; !ok {
			t.Errorf("%q is offered in the UI but has no pricing entry", m)
		}
	}
}
