package provider

import "os"

// ProviderOpenRouter is an aggregator: one key reaches many upstream model
// families. It is the only provider whose model-list endpoint returns pricing
// and capability together, which is why it is the one Kiwi funds.
const ProviderOpenRouter = "openrouter"

// Kind selects the client used to talk to a provider. Native providers have
// their own request/response shapes; openai_compatible providers are served by
// the OpenAI client pointed at a different base URL.
const (
	KindNative           = "native"
	KindOpenAICompatible = "openai_compatible"
)

// Spec is everything Kiwi needs to know about a provider. It is the single
// definition: credential naming, display, transport, discovery, and whether
// Kiwi holds a key of its own all read from here.
//
// This exists because the same five facts were previously restated in
// CredentialNameFor, DisplayNameFor, integrationSpec, daemon.isLLMKey and the
// frontend — five places to edit, and four of them easy to forget.
type Spec struct {
	ID       string
	Display  string
	CredName string
	Kind     string
	BaseURL  string
	// ModelsPath is the discovery endpoint, relative to BaseURL.
	ModelsPath string
	// PlatformEnv names the environment variable holding Kiwi's own key for
	// this provider. Unset or empty at runtime means the provider is BYOK-only
	// and the UI shows "Coming soon".
	PlatformEnv string
}

var registry = []Spec{
	{
		ID: ProviderAnthropic, Display: "Anthropic", CredName: "ANTHROPIC_API_KEY",
		Kind: KindNative, BaseURL: "https://api.anthropic.com", ModelsPath: "/v1/models",
		PlatformEnv: "KIWI_PLATFORM_ANTHROPIC_API_KEY",
	},
	{
		ID: ProviderGemini, Display: "Gemini", CredName: "GEMINI_API_KEY",
		Kind: KindNative, BaseURL: defaultGeminiBaseURL, ModelsPath: "/models",
		PlatformEnv: "KIWI_PLATFORM_GEMINI_API_KEY",
	},
	{
		ID: ProviderOpenAI, Display: "OpenAI", CredName: "OPENAI_API_KEY",
		Kind: KindNative, BaseURL: defaultOpenAIBaseURL, ModelsPath: "/models",
		PlatformEnv: "KIWI_PLATFORM_OPENAI_API_KEY",
	},
	{
		ID: ProviderOpenRouter, Display: "OpenRouter", CredName: "OPENROUTER_API_KEY",
		Kind: KindOpenAICompatible, BaseURL: "https://openrouter.ai/api/v1", ModelsPath: "/models",
		PlatformEnv: "KIWI_PLATFORM_OPENROUTER_API_KEY",
	},
}

// Registry returns every known provider. The slice is a copy: callers must not
// be able to mutate the one definition.
func Registry() []Spec {
	out := make([]Spec, len(registry))
	copy(out, registry)
	return out
}

// SpecFor looks up a provider by canonical id.
func SpecFor(id string) (Spec, bool) {
	for _, s := range registry {
		if s.ID == id {
			return s, true
		}
	}
	return Spec{}, false
}

// IsLLMCredential reports whether a credential name holds a model API key.
//
// This is what keeps model keys out of the sandbox that runs model-generated
// code, so it is deliberately exact-match against the registry rather than a
// prefix rule: a provider added without a registry row fails the parity test
// rather than silently widening what the container can see.
func IsLLMCredential(name string) bool {
	if name == "" {
		return false
	}
	for _, s := range registry {
		if s.Kind != "" && s.CredName == name {
			return true
		}
	}
	return false
}

// PlatformKeyFor returns Kiwi's own key for a provider, and whether one is
// configured. An unset variable is the mechanism behind "Coming soon".
func PlatformKeyFor(id string) (string, bool) {
	spec, ok := SpecFor(id)
	if !ok || spec.PlatformEnv == "" {
		return "", false
	}
	key := os.Getenv(spec.PlatformEnv)
	return key, key != ""
}
