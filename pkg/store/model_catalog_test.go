package store

import (
	"context"
	"testing"
	"time"
)

func f64(v float64) *float64 { return &v }
func i(v int) *int           { return &v }
func b(v bool) *bool         { return &v }

// The catalog is keyed (org_id, model_id) with "" meaning global. An org row
// and a global row for the same model id must be able to coexist: the org row
// describes access that org has and others do not.
func TestCatalogGlobalAndOrgRowsCoexist(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC()

	global := &CatalogModel{
		OrgID: GlobalCatalogOrg, ModelID: "gpt-5-mini", Provider: "openai",
		Source: "discovered", FirstSeenAt: now, LastSeenAt: now,
	}
	orgRow := &CatalogModel{
		OrgID: "o1", ModelID: "gpt-5-mini", Provider: "openai",
		Source: "discovered", FirstSeenAt: now, LastSeenAt: now,
	}
	if err := s.UpsertCatalogModel(ctx, global); err != nil {
		t.Fatalf("upsert global: %v", err)
	}
	if err := s.UpsertCatalogModel(ctx, orgRow); err != nil {
		t.Fatalf("upsert org row: %v", err)
	}

	got, err := s.ListCatalogModels(ctx, "o1")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("ListCatalogModels(o1) returned %d rows, want 2", len(got))
	}
}

// An org must never see another org's discovered models.
func TestCatalogIsOrgScoped(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC()

	if err := s.UpsertCatalogModel(ctx, &CatalogModel{
		OrgID: "o1", ModelID: "ft:private-model", Provider: "openai",
		Source: "discovered", FirstSeenAt: now, LastSeenAt: now,
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	got, err := s.ListCatalogModels(ctx, "o2")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	for _, m := range got {
		if m.ModelID == "ft:private-model" {
			t.Fatal("org o2 can see org o1's discovered model")
		}
	}
}

// Upsert is how refresh works: the same model seen again updates in place
// rather than duplicating, and FirstSeenAt is preserved.
func TestCatalogUpsertIsIdempotent(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	first := time.Now().UTC().Add(-48 * time.Hour)
	later := time.Now().UTC()

	m := &CatalogModel{
		OrgID: GlobalCatalogOrg, ModelID: "kimi-k2", Provider: "openrouter",
		OutputCostPerM: f64(2.50), Source: "discovered",
		FirstSeenAt: first, LastSeenAt: first,
	}
	if err := s.UpsertCatalogModel(ctx, m); err != nil {
		t.Fatalf("first upsert: %v", err)
	}

	m2 := &CatalogModel{
		OrgID: GlobalCatalogOrg, ModelID: "kimi-k2", Provider: "openrouter",
		OutputCostPerM: f64(1.75), Source: "discovered",
		FirstSeenAt: later, LastSeenAt: later,
	}
	if err := s.UpsertCatalogModel(ctx, m2); err != nil {
		t.Fatalf("second upsert: %v", err)
	}

	got, err := s.GetCatalogModel(ctx, GlobalCatalogOrg, "kimi-k2")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.OutputCostPerM == nil || *got.OutputCostPerM != 1.75 {
		t.Errorf("OutputCostPerM not updated: %v", got.OutputCostPerM)
	}
	if !got.FirstSeenAt.Equal(first) {
		t.Errorf("FirstSeenAt = %v, want preserved %v", got.FirstSeenAt, first)
	}
	if !got.LastSeenAt.Equal(later) {
		t.Errorf("LastSeenAt = %v, want %v", got.LastSeenAt, later)
	}
}
