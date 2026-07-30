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
)

// ProviderOf maps a model id to the provider that serves it, mirroring
// providerOf() in the dashboard. Anything not recognisably Gemini is treated as
// Anthropic, which is the same fallback ModelCostUSD applies to pricing.
func ProviderOf(model string) string {
	if strings.HasPrefix(model, "gemini") {
		return ProviderGemini
	}
	return ProviderAnthropic
}
