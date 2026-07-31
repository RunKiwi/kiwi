package orchestrator

import (
	"testing"

	"github.com/ibreakthecloud/kiwi/pkg/provider"
)

// The integrations catalog is what the dashboard renders as connectable, and
// what /api/v1/integrations reports as connected. A provider the daemon can
// route to but the catalog omits is unreachable in practice: there is no way to
// save its key, so every task on its models fails asking for a credential the UI
// never offered.
func TestEveryProviderIsConnectable(t *testing.T) {
	inCatalog := map[string]bool{}
	for _, spec := range integrationSpec {
		inCatalog[spec.CredName] = true
	}

	for _, p := range []string{provider.ProviderAnthropic, provider.ProviderGemini, provider.ProviderOpenAI} {
		name := provider.CredentialNameFor(p)
		if !inCatalog[name] {
			t.Errorf("provider %q needs %s, but the integrations catalog does not offer it", p, name)
		}
	}
}

// Every LLM entry in the catalog must be classified "llm". The kind is what the
// dashboard groups on and what the credential is stored under; a mislabelled
// entry saves fine and then is not found where the planner looks.
func TestLLMIntegrationsAreClassified(t *testing.T) {
	want := map[string]bool{"ANTHROPIC_API_KEY": true, "GEMINI_API_KEY": true, "OPENAI_API_KEY": true}
	for _, spec := range integrationSpec {
		if want[spec.CredName] && spec.Kind != "llm" {
			t.Errorf("%s has kind %q, want llm", spec.CredName, spec.Kind)
		}
	}
}

// inferProvider labels a model on the Models page; providerForModel names the
// provider in the signed execution record. Both must agree with the routing the
// daemon actually performs — a record naming a provider the task did not run on
// is a false attestation.
func TestModelLabellingMatchesDaemonRouting(t *testing.T) {
	for _, model := range []string{
		"claude-opus-4-8", "gemini-flash-latest", "gpt-5", "gpt-4o-mini", "o3-mini", "something-unknown",
	} {
		want := provider.ProviderOf(model)
		if got := inferProvider(model); got != want {
			t.Errorf("inferProvider(%q) = %q, want %q", model, got, want)
		}
		if got := providerForModel(model); got != want {
			t.Errorf("providerForModel(%q) = %q, want %q", model, got, want)
		}
	}
}

// A step that named no model must not have a provider invented for it.
func TestProviderForEmptyModelStaysEmpty(t *testing.T) {
	if got := providerForModel(""); got != "" {
		t.Errorf("providerForModel(\"\") = %q, want empty", got)
	}
}
