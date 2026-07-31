package provider

import "strings"

// Canonical provider identifiers. These are the strings that appear in stored
// credentials, the integrations catalog, the dashboard, and the signed
// execution record — so they have to agree everywhere. "google" is not one of
// them; the provider is named for its model family, matching GEMINI_API_KEY and
// the "gemini" integration key.
const (
	ProviderAnthropic = "anthropic"
	ProviderGemini    = "gemini"
	ProviderOpenAI    = "openai"
)

// openaiModelPrefixes are the model-id prefixes that route to OpenAI.
//
// It is a prefix list rather than a single letter because "o" alone would
// swallow far too much — a future "opus-…" or an org's own alias — and a wrong
// match here sends a task to a provider whose key the org may not even have
// connected, failing it with a confusing reason.
var openaiModelPrefixes = []string{"gpt-", "gpt3", "gpt4", "o1", "o3", "o4", "chatgpt", "text-embedding-3"}

// CredentialNameFor returns the stored credential name that holds a provider's
// API key. It is the single definition of that mapping: the planner, the daemon
// and the integrations catalog all resolve keys through the same table, so
// adding a provider cannot leave one of them behind.
func CredentialNameFor(providerID string) string {
	switch providerID {
	case ProviderGemini:
		return "GEMINI_API_KEY"
	case ProviderOpenAI:
		return "OPENAI_API_KEY"
	default:
		return "ANTHROPIC_API_KEY"
	}
}

// DisplayNameFor returns the provider's name as written for a human, for use in
// error messages shown on a job.
func DisplayNameFor(providerID string) string {
	switch providerID {
	case ProviderGemini:
		return "Gemini"
	case ProviderOpenAI:
		return "OpenAI"
	default:
		return "Anthropic"
	}
}

// ProviderOf maps a model id to the provider that serves it. It is the one
// implementation of that routing in the Go tree — the daemon, the planner, the
// dashboard API and the execution-record hook all call it — because the same
// model must never be attributed to one provider for billing, another for key
// lookup, and a third in the signed record.
//
// Anything not recognisably Gemini or OpenAI is treated as Anthropic, which is
// the same fallback ModelCostUSD applies to pricing.
func ProviderOf(model string) string {
	m := strings.ToLower(model)
	if strings.HasPrefix(m, "gemini") {
		return ProviderGemini
	}
	for _, prefix := range openaiModelPrefixes {
		if strings.HasPrefix(m, prefix) {
			return ProviderOpenAI
		}
	}
	return ProviderAnthropic
}
