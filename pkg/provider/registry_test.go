package provider

import "testing"

func TestRegistryCoversAllProviders(t *testing.T) {
	want := map[string]string{
		ProviderAnthropic:  "ANTHROPIC_API_KEY",
		ProviderGemini:     "GEMINI_API_KEY",
		ProviderOpenAI:     "OPENAI_API_KEY",
		ProviderOpenRouter: "OPENROUTER_API_KEY",
	}
	for id, credName := range want {
		spec, ok := SpecFor(id)
		if !ok {
			t.Fatalf("SpecFor(%q): not found in registry", id)
		}
		if spec.CredName != credName {
			t.Errorf("SpecFor(%q).CredName = %q, want %q", id, spec.CredName, credName)
		}
		if spec.Display == "" {
			t.Errorf("SpecFor(%q).Display is empty", id)
		}
		if spec.BaseURL == "" {
			t.Errorf("SpecFor(%q).BaseURL is empty", id)
		}
		if spec.ModelsPath == "" {
			t.Errorf("SpecFor(%q).ModelsPath is empty", id)
		}
	}
	if len(Registry()) != len(want) {
		t.Errorf("Registry() has %d rows, want %d", len(Registry()), len(want))
	}
}

func TestSpecForUnknownProvider(t *testing.T) {
	if _, ok := SpecFor("nope"); ok {
		t.Error("SpecFor(\"nope\") returned ok=true, want false")
	}
}

// IsLLMCredential is what keeps a model key out of the sandbox. A credential
// name that is not an LLM key must never be reported as one, and every
// registry row's key must be recognised.
func TestIsLLMCredential(t *testing.T) {
	for _, name := range []string{"ANTHROPIC_API_KEY", "GEMINI_API_KEY", "OPENAI_API_KEY", "OPENROUTER_API_KEY"} {
		if !IsLLMCredential(name) {
			t.Errorf("IsLLMCredential(%q) = false, want true", name)
		}
	}
	for _, name := range []string{"GITHUB_TOKEN", "SLACK_TOKEN", "GIT_TOKEN", "", "ANTHROPIC"} {
		if IsLLMCredential(name) {
			t.Errorf("IsLLMCredential(%q) = true, want false", name)
		}
	}
}

func TestOpenRouterIsOpenAICompatible(t *testing.T) {
	spec, _ := SpecFor(ProviderOpenRouter)
	if spec.Kind != KindOpenAICompatible {
		t.Errorf("openrouter Kind = %q, want %q", spec.Kind, KindOpenAICompatible)
	}
	if spec.PlatformEnv != "KIWI_PLATFORM_OPENROUTER_API_KEY" {
		t.Errorf("openrouter PlatformEnv = %q", spec.PlatformEnv)
	}
}

// The Anthropic fallback for an unrecognised provider id is load-bearing:
// ProviderOf returns anthropic for anything it does not recognise, and these
// two functions must agree with it. Registry lookup must not change that.
func TestNamingFallsBackToAnthropic(t *testing.T) {
	if got := CredentialNameFor("not-a-provider"); got != "ANTHROPIC_API_KEY" {
		t.Errorf("CredentialNameFor(unknown) = %q, want ANTHROPIC_API_KEY", got)
	}
	if got := DisplayNameFor("not-a-provider"); got != "Anthropic" {
		t.Errorf("DisplayNameFor(unknown) = %q, want Anthropic", got)
	}
	if got := CredentialNameFor(""); got != "ANTHROPIC_API_KEY" {
		t.Errorf("CredentialNameFor(\"\") = %q, want ANTHROPIC_API_KEY", got)
	}
}

func TestNamingResolvesOpenRouter(t *testing.T) {
	if got := CredentialNameFor(ProviderOpenRouter); got != "OPENROUTER_API_KEY" {
		t.Errorf("CredentialNameFor(openrouter) = %q", got)
	}
	if got := DisplayNameFor(ProviderOpenRouter); got != "OpenRouter" {
		t.Errorf("DisplayNameFor(openrouter) = %q", got)
	}
}
