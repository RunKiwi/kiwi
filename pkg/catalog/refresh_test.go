package catalog

import (
	"context"
	"fmt"
	"testing"

	"github.com/ibreakthecloud/kiwi/pkg/store"
)

type mockStore struct {
	store.Store // panic on unexpected calls
	upserts     []store.CatalogModel
	orgCreds    []store.Credential
}

func (m *mockStore) UpsertCatalogModel(ctx context.Context, model *store.CatalogModel) error {
	m.upserts = append(m.upserts, *model)
	return nil
}

func (m *mockStore) ListCredentials(ctx context.Context, orgID string) ([]store.Credential, error) {
	return m.orgCreds, nil
}

func (m *mockStore) GetCredentialPlaintext(ctx context.Context, orgID, name string) (string, error) {
	return "fake-key", nil
}

type mockLister struct {
	models []DiscoveredModel
	err    error
}

func (m *mockLister) List(ctx context.Context, baseURL, apiKey string) ([]DiscoveredModel, error) {
	return m.models, m.err
}

func TestRefreshPlatform(t *testing.T) {
	cfg := RefresherConfig{
		PlatformListers: map[string]Lister{
			"openrouter": &mockLister{
				models: []DiscoveredModel{
					{ID: "m1", SupportsTools: ptrB(true), ContextLength: ptrI(128000), Modality: "text->text", InputCostPerM: ptrF(0.60), OutputCostPerM: ptrF(2.50)},
					{ID: "m2"},
				},
			},
		},
	}
	s := &mockStore{}
	r := NewRefresher(s, cfg)

	if err := r.RefreshPlatform(context.Background()); err != nil {
		t.Fatalf("RefreshPlatform: %v", err)
	}

	if len(s.upserts) != 2 {
		t.Fatalf("got %d upserts, want 2", len(s.upserts))
	}
	m1 := s.upserts[0]
	if m1.OrgID != store.GlobalCatalogOrg {
		t.Errorf("m1.OrgID = %q, want global", m1.OrgID)
	}
	if m1.Provider != "openrouter" {
		t.Errorf("m1.Provider = %q, want openrouter", m1.Provider)
	}
	if m1.Tier == store.TierUnknown {
		t.Error("m1 Tier is unknown; capability parsing failed")
	}
}

func TestRefreshPlatformIgnoresProviderError(t *testing.T) {
	cfg := RefresherConfig{
		PlatformListers: map[string]Lister{
			"failing": &mockLister{err: fmt.Errorf("timeout")},
			"working": &mockLister{models: []DiscoveredModel{{ID: "m1"}}},
		},
	}
	s := &mockStore{}
	if err := NewRefresher(s, cfg).RefreshPlatform(context.Background()); err != nil {
		t.Fatalf("RefreshPlatform returned error %v, want nil", err)
	}
	if len(s.upserts) != 1 {
		t.Fatalf("working provider was not upserted: got %d upserts", len(s.upserts))
	}
}

func TestRefreshOrg(t *testing.T) {
	cfg := RefresherConfig{
		NativeListers: map[string]Lister{
			"openai": &mockLister{models: []DiscoveredModel{{ID: "gpt-4o"}}},
		},
	}
	s := &mockStore{
		orgCreds: []store.Credential{{Name: "openai_api_key", Kind: "openai"}},
	}
	r := NewRefresher(s, cfg)

	if err := r.RefreshOrg(context.Background(), "o1"); err != nil {
		t.Fatalf("RefreshOrg: %v", err)
	}

	if len(s.upserts) != 1 {
		t.Fatalf("got %d upserts, want 1", len(s.upserts))
	}
	got := s.upserts[0]
	if got.OrgID != "o1" {
		t.Errorf("OrgID = %q, want o1", got.OrgID)
	}
	if got.ModelID != "gpt-4o" {
		t.Errorf("ModelID = %q, want gpt-4o", got.ModelID)
	}
	// A native BYOK model starts out TierUnknown because the native lister
	// returns no pricing, and tests don't wire PricingLookup.
	if got.Tier != store.TierUnknown {
		t.Errorf("Tier = %q, want %q", got.Tier, store.TierUnknown)
	}
	// And an unpriceable model must never be KiwiProvided.
	if got.KiwiProvided {
		t.Error("KiwiProvided = true, want false")
	}
	if !got.Selectable {
		t.Error("Selectable = false on a BYOK model; BYOK bypasses capability checks")
	}
}

// A missing credential must not fail the refresh, it just skips that provider.
func TestRefreshOrgSkipsMissingCredential(t *testing.T) {
	lister := &mockLister{models: []DiscoveredModel{{ID: "gpt-4o"}}}
	cfg := RefresherConfig{
		NativeListers: map[string]Lister{"openai": lister},
	}
	s := &mockStore{} // no credentials
	r := NewRefresher(s, cfg)

	if err := r.RefreshOrg(context.Background(), "o1"); err != nil {
		t.Fatalf("RefreshOrg: %v", err)
	}
	if len(s.upserts) != 0 {
		t.Errorf("got %d upserts, want 0", len(s.upserts))
	}
}
