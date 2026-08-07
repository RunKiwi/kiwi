package daemon

import (
	"testing"

	"github.com/ibreakthecloud/kiwi/pkg/provider"
)

// The daemon picks the Actor/Critic from the worker's model and the key from the
// sealed bundle. Getting the pairing wrong does not fail loudly at selection —
// it fails at the provider API with someone else's key, reported to the user as
// an authentication error against a provider they never chose.
func TestDefaultProviderRoutesModelToItsOwnKey(t *testing.T) {
	creds := map[string]string{
		"ANTHROPIC_API_KEY": "sk-ant-x",
		"GEMINI_API_KEY":    "AIza-x",
		"OPENAI_API_KEY":    "sk-openai-x",
	}

	cases := map[string]any{
		"claude-opus-4-8":     &provider.AnthropicProvider{},
		"gemini-flash-latest": &provider.GeminiProvider{},
		"gpt-5-mini":          &provider.OpenAIProvider{},
		"gpt-4o":              &provider.OpenAIProvider{},
		"o3-mini":             &provider.OpenAIProvider{},
	}

	for model, want := range cases {
		actor, critic := defaultProvider(creds, model, "")
		if actor == nil {
			t.Errorf("model %q: no provider selected despite every key being present", model)
			continue
		}
		if _, ok := actor.(interface{ LastCostUSD() float64 }); !ok {
			t.Errorf("model %q: provider cannot report cost, so the budget cap cannot bind", model)
		}
		gotType := typeName(actor)
		if gotType != typeName(want) {
			t.Errorf("model %q routed to %s, want %s", model, gotType, typeName(want))
		}
		// One instance serves both roles; a nil Critic would silently skip review.
		if critic == nil {
			t.Errorf("model %q: no critic selected", model)
		}
	}
}

func typeName(v any) string {
	switch v.(type) {
	case *provider.AnthropicProvider:
		return "anthropic"
	case *provider.GeminiProvider:
		return "gemini"
	case *provider.OpenAIProvider:
		return "openai"
	default:
		return "unknown"
	}
}

// A model whose provider has no key must return nil so executeTask can fail with
// a precise reason, rather than constructing a provider around an empty key and
// failing later as a bare 401.
func TestDefaultProviderWithoutTheMatchingKey(t *testing.T) {
	onlyAnthropic := map[string]string{"ANTHROPIC_API_KEY": "sk-ant-x"}

	for _, model := range []string{"gpt-5-mini", "gemini-2.0-flash"} {
		if actor, _ := defaultProvider(onlyAnthropic, model, ""); actor != nil {
			t.Errorf("model %q built a provider with no key for it", model)
		}
	}
	if actor, _ := defaultProvider(onlyAnthropic, "claude-opus-4-8", ""); actor == nil {
		t.Error("the one connected provider should still work")
	}
}

// The message names the provider whose key is missing, and it must name the same
// one defaultProvider actually looked up — otherwise it sends the user to add a
// key that would not have helped.
func TestProviderNameMatchesTheKeyThatWasLookedUp(t *testing.T) {
	cases := map[string]string{
		"gpt-5-mini":          "OpenAI",
		"gemini-flash-latest": "Gemini",
		"claude-opus-4-8":     "Anthropic",
	}
	for model, want := range cases {
		if got := providerNameForModel(model); got != want {
			t.Errorf("providerNameForModel(%q) = %q, want %q", model, got, want)
		}
		// The display name and the credential lookup must describe one provider.
		if provider.DisplayNameFor(provider.ProviderOf(model)) != want {
			t.Errorf("model %q: display name disagrees with the routed provider", model)
		}
	}
}

// LLM keys are deliberately withheld from the sandbox: the Actor/Critic run in
// the daemon process, so the container executing model-generated code must never
// hold a model key. A provider added without an entry here would leak one.
func TestEveryProviderKeyIsWithheldFromTheSandbox(t *testing.T) {
	for _, p := range []string{provider.ProviderAnthropic, provider.ProviderGemini, provider.ProviderOpenAI} {
		name := provider.CredentialNameFor(p)
		if !isLLMKey(name) {
			t.Errorf("%s (%s) would be passed into the sandbox environment", name, p)
		}
	}
	// Non-model credentials still reach the test command, which needs them.
	for _, name := range []string{"GITHUB_TOKEN", "GIT_TOKEN", "SLACK_TOKEN", "DATABASE_URL"} {
		if isLLMKey(name) {
			t.Errorf("%s is not a model key and must stay available to the test command", name)
		}
	}
}

// A provider in the registry whose key is not recognised here would have its
// key handed to the container that runs model-generated code. This asserts the
// two can never drift.
func TestIsLLMKeyCoversEveryRegistryProvider(t *testing.T) {
	for _, spec := range provider.Registry() {
		if spec.Kind == "" {
			continue
		}
		if !isLLMKey(spec.CredName) {
			t.Errorf("isLLMKey(%q) = false for registry provider %q; the key would leak into the sandbox", spec.CredName, spec.ID)
		}
	}
}

func TestIsLLMKeyRejectsNonModelCredentials(t *testing.T) {
	for _, name := range []string{"GITHUB_TOKEN", "GIT_TOKEN", "SLACK_TOKEN", ""} {
		if isLLMKey(name) {
			t.Errorf("isLLMKey(%q) = true, want false", name)
		}
	}
}
