package catalog

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ibreakthecloud/kiwi/pkg/provider"
	"github.com/ibreakthecloud/kiwi/pkg/store"
)

// fakeStore records what the refresher did, without a database.
type fakeStore struct {
	creds     []store.Credential
	plaintext map[string]string

	upserted  []store.CatalogModel
	markCalls int
	markedSet []string
}

func newFakeStore() *fakeStore {
	return &fakeStore{plaintext: map[string]string{}}
}

func (f *fakeStore) ListCredentials(context.Context, string) ([]store.Credential, error) {
	return f.creds, nil
}

func (f *fakeStore) GetCredentialPlaintext(_ context.Context, _, name string) (string, error) {
	return f.plaintext[name], nil
}

func (f *fakeStore) UpsertCatalogModel(_ context.Context, m *store.CatalogModel) error {
	f.upserted = append(f.upserted, *m)
	return nil
}

func (f *fakeStore) MarkCatalogMissing(_ context.Context, _, _ string, seen []string, _ time.Time) error {
	f.markCalls++
	f.markedSet = seen
	return nil
}

type stubLister struct {
	models []DiscoveredModel
	err    error
}

func (s stubLister) List(context.Context, string, string) ([]DiscoveredModel, error) {
	return s.models, s.err
}

func openRouterSpec(t *testing.T) provider.Spec {
	t.Helper()
	spec, ok := provider.SpecFor(provider.ProviderOpenRouter)
	if !ok {
		t.Fatal("openrouter missing from registry")
	}
	return spec
}

// THE fail-safe. A provider outage must never empty every user's model picker.
func TestRefreshLeavesRowsUntouchedOnError(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
	}{
		{"transport error", errors.New("dial tcp: connection refused")},
		{"server error", errors.New("status 503")},
		{"malformed body", errors.New("decode: invalid character 'n'")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fs := newFakeStore()
			r := NewRefresher(fs)

			err := r.refresh(context.Background(), store.GlobalCatalogOrg,
				openRouterSpec(t), stubLister{err: tc.err}, "k", true)
			if err == nil {
				t.Fatal("refresh returned nil on a failed list")
			}
			if len(fs.upserted) != 0 {
				t.Errorf("wrote %d rows despite a failed list", len(fs.upserted))
			}
			if fs.markCalls != 0 {
				t.Error("marked models missing despite a failed list; one outage would empty every picker")
			}
		})
	}
}

// An empty but SUCCESSFUL list is different: the provider really is serving
// nothing, so absence is meaningful and the rows should be marked.
func TestRefreshMarksMissingOnEmptySuccess(t *testing.T) {
	fs := newFakeStore()
	r := NewRefresher(fs)

	if err := r.refresh(context.Background(), store.GlobalCatalogOrg,
		openRouterSpec(t), stubLister{models: nil}, "k", true); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if fs.markCalls != 1 {
		t.Errorf("MarkCatalogMissing called %d times, want 1", fs.markCalls)
	}
	if len(fs.markedSet) != 0 {
		t.Errorf("seen list has %d ids, want 0", len(fs.markedSet))
	}
}

func TestRefreshAppliesDerivedFields(t *testing.T) {
	fs := newFakeStore()
	r := NewRefresher(fs)

	err := r.refresh(context.Background(), store.GlobalCatalogOrg, openRouterSpec(t), stubLister{models: []DiscoveredModel{
		{ID: "cheap", ContextLength: ptrI(128000), Modality: "text->text",
			SupportsTools: ptrB(true), InputCostPerM: ptrF(0.20), OutputCostPerM: ptrF(1.00)},
		{ID: "pricey", ContextLength: ptrI(200000), Modality: "text->text",
			SupportsTools: ptrB(true), InputCostPerM: ptrF(15), OutputCostPerM: ptrF(75)},
	}}, "k", true)
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}

	byID := map[string]store.CatalogModel{}
	for _, m := range fs.upserted {
		byID[m.ModelID] = m
	}
	if byID["cheap"].Tier != store.TierEconomy {
		t.Errorf("cheap Tier = %q, want %q", byID["cheap"].Tier, store.TierEconomy)
	}
	if byID["pricey"].Tier != store.TierFrontier {
		t.Errorf("pricey Tier = %q, want %q", byID["pricey"].Tier, store.TierFrontier)
	}
	if !byID["cheap"].KiwiProvided || !byID["cheap"].Selectable {
		t.Error("a priceable, capable model was not marked kiwi_provided and selectable")
	}
	if fs.markCalls != 1 || len(fs.markedSet) != 2 {
		t.Errorf("markCalls=%d seen=%d, want 1 and 2", fs.markCalls, len(fs.markedSet))
	}
}

// An org's own discovery describes what THAT ORG can reach with its own key.
// It says nothing about what Kiwi would pay for.
func TestRefreshOrgRowsAreNeverKiwiFunded(t *testing.T) {
	fs := newFakeStore()
	r := NewRefresher(fs)

	err := r.refresh(context.Background(), "o1", openRouterSpec(t), stubLister{models: []DiscoveredModel{
		{ID: "cheap", ContextLength: ptrI(128000), Modality: "text->text",
			SupportsTools: ptrB(true), InputCostPerM: ptrF(0.20), OutputCostPerM: ptrF(1.00)},
	}}, "org-key", false)
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if fs.upserted[0].KiwiProvided {
		t.Error("a model discovered with the customer's own key was marked kiwi_provided")
	}
}

// Capability must never be fabricated. It used to be: every org-discovered row
// was stamped SupportsTools=true, ContextLength=128000, Modality=text->text,
// which made DeriveSelectable unreachable and put whisper, tts and embedding
// models straight into the picker.
func TestRefreshDoesNotFabricateCapability(t *testing.T) {
	fs := newFakeStore()
	r := NewRefresher(fs)

	openai, _ := provider.SpecFor(provider.ProviderOpenAI)
	err := r.refresh(context.Background(), "o1", openai, stubLister{models: []DiscoveredModel{
		{ID: "text-embedding-3-small"},
		{ID: "whisper-1"},
		{ID: "gpt-5-mini"},
	}}, "sk-org", false)
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}

	byID := map[string]store.CatalogModel{}
	for _, m := range fs.upserted {
		byID[m.ModelID] = m
	}
	for _, id := range []string{"text-embedding-3-small", "whisper-1"} {
		if byID[id].Selectable {
			t.Errorf("%s is selectable; it cannot drive the Actor–Critic loop", id)
		}
	}
	// A real chat model still comes through, priced from PricingMap.
	if !byID["gpt-5-mini"].Selectable {
		t.Error("gpt-5-mini is not selectable; the capability table did not apply")
	}
	if byID["gpt-5-mini"].Tier == store.TierUnknown {
		t.Error("gpt-5-mini has no tier; the PricingMap join did not run")
	}
}

// Providers are resolved from the credential NAME. Resolving by Kind matched
// nothing — Kind is "llm" or "git", never a provider id — so every org refresh
// silently discovered zero models.
func TestRefreshOrgResolvesProviderFromCredentialName(t *testing.T) {
	fs := newFakeStore()
	fs.creds = []store.Credential{
		// The realistic row shape: Kind is a category, not a provider.
		{Name: "OPENAI_API_KEY", Kind: "llm"},
		{Name: "GITHUB_TOKEN", Kind: "git"},
	}
	fs.plaintext["OPENAI_API_KEY"] = "sk-org"

	if _, ok := specForCredentialName("OPENAI_API_KEY"); !ok {
		t.Fatal("OPENAI_API_KEY did not resolve to a provider")
	}
	if _, ok := specForCredentialName("GITHUB_TOKEN"); ok {
		t.Error("GITHUB_TOKEN resolved to a provider; it is not an LLM key")
	}
	if _, ok := specForCredentialName("llm"); ok {
		t.Error("the credential Kind resolved to a provider; only the Name should")
	}
	if _, ok := specForCredentialName(""); ok {
		t.Error("an empty credential name resolved to a provider")
	}
}

func TestSpecForCredentialNameCoversEveryProvider(t *testing.T) {
	for _, spec := range provider.Registry() {
		got, ok := specForCredentialName(spec.CredName)
		if !ok {
			t.Errorf("%s did not resolve to a provider", spec.CredName)
			continue
		}
		if got.ID != spec.ID {
			t.Errorf("%s resolved to %q, want %q", spec.CredName, got.ID, spec.ID)
		}
	}
}

// With no platform key configured, a provider is skipped entirely — that skip
// is what makes an unset KIWI_PLATFORM_* mean "Coming soon".
func TestRefreshPlatformSkipsProvidersWithNoKey(t *testing.T) {
	for _, spec := range provider.Registry() {
		if spec.PlatformEnv != "" {
			t.Setenv(spec.PlatformEnv, "")
		}
	}
	fs := newFakeStore()
	NewRefresher(fs).RefreshPlatform(context.Background())

	if len(fs.upserted) != 0 || fs.markCalls != 0 {
		t.Errorf("refreshed with no platform key configured: %d upserts, %d marks",
			len(fs.upserted), fs.markCalls)
	}
}
