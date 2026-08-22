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

func TestDeriveTier(t *testing.T) {
	cases := []struct {
		name    string
		in, out *float64
		want    string
	}{
		{"both zero is free", f64(0), f64(0), TierFree},
		{"cheap output is economy", f64(0.30), f64(1.20), TierEconomy},
		{"exactly at the ceiling is economy", f64(1), f64(2.00), TierEconomy},
		{"just over the ceiling is frontier", f64(1), f64(2.01), TierFrontier},
		{"expensive is frontier", f64(15), f64(75), TierFrontier},
		{"nil output is unknown", f64(1), nil, TierUnknown},
		{"nil input is unknown", nil, f64(1), TierUnknown},
		{"both nil is unknown", nil, nil, TierUnknown},
		// Free input with paid output is not free. Getting this backwards
		// would put a paid model in the free bucket and spend real money.
		{"zero input paid output is not free", f64(0), f64(3.00), TierFrontier},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := DeriveTier(tc.in, tc.out); got != tc.want {
				t.Errorf("DeriveTier(%v, %v) = %q, want %q", tc.in, tc.out, got, tc.want)
			}
		})
	}
}

// Each condition must be able to fail the model on its own. The loop returns
// whole files as JSON and drives the Critic through tool calls, so a model
// missing any of these fails partway through a task rather than degrading.
func TestDeriveSelectable(t *testing.T) {
	ok := func() *CatalogModel {
		return &CatalogModel{
			SupportsTools: b(true), ContextLength: i(128000), Modality: "text->text",
		}
	}
	if !DeriveSelectable(ok()) {
		t.Fatal("a fully-qualified model is not selectable")
	}

	noTools := ok()
	noTools.SupportsTools = b(false)
	if DeriveSelectable(noTools) {
		t.Error("a model without tool support is selectable")
	}

	unknownTools := ok()
	unknownTools.SupportsTools = nil
	if DeriveSelectable(unknownTools) {
		t.Error("a model with unknown tool support is selectable")
	}

	small := ok()
	small.ContextLength = i(8192)
	if DeriveSelectable(small) {
		t.Error("a model below the context floor is selectable")
	}

	atFloor := ok()
	atFloor.ContextLength = i(32000)
	if !DeriveSelectable(atFloor) {
		t.Error("a model exactly at the context floor is not selectable")
	}

	wrongModality := ok()
	wrongModality.Modality = "text->image"
	if DeriveSelectable(wrongModality) {
		t.Error("a non text->text model is selectable")
	}

	gone := ok()
	now := time.Now().UTC()
	gone.MissingSince = &now
	if DeriveSelectable(gone) {
		t.Error("a model missing from its provider's list is selectable")
	}
}

// Kiwi never funds a model it cannot price, even when a platform key exists.
func TestApplyDerivedNeverFundsUnpriceableModels(t *testing.T) {
	m := &CatalogModel{
		Provider: "openrouter", SupportsTools: b(true),
		ContextLength: i(128000), Modality: "text->text",
		InputCostPerM: nil, OutputCostPerM: nil,
	}
	m.ApplyDerived(true)
	if m.Tier != TierUnknown {
		t.Errorf("Tier = %q, want %q", m.Tier, TierUnknown)
	}
	if m.KiwiProvided {
		t.Error("an unpriceable model was marked kiwi_provided")
	}
	// It is still usable on the customer's own key, where they pay.
	if !m.Selectable {
		t.Error("an unpriceable but capable model should stay selectable for BYOK")
	}
}

func TestApplyDerivedFundsPriceableModelWithKey(t *testing.T) {
	m := &CatalogModel{
		Provider: "openrouter", SupportsTools: b(true),
		ContextLength: i(128000), Modality: "text->text",
		InputCostPerM: f64(0.60), OutputCostPerM: f64(2.50),
	}
	m.ApplyDerived(true)
	if m.Tier != TierFrontier {
		t.Errorf("Tier = %q, want %q", m.Tier, TierFrontier)
	}
	if !m.KiwiProvided {
		t.Error("a priceable model with a platform key was not marked kiwi_provided")
	}

	m.ApplyDerived(false)
	if m.KiwiProvided {
		t.Error("kiwi_provided stayed true with no platform key configured")
	}
}

// A catalog hit is authoritative.
func TestResolveModelFromCatalog(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC()

	// kimi-k2 has no prefix that ProviderOf recognises, so inference would
	// call it Anthropic. The catalog is what makes it route correctly.
	if err := s.UpsertCatalogModel(ctx, &CatalogModel{
		OrgID: GlobalCatalogOrg, ModelID: "kimi-k2", Provider: "openrouter",
		Tier: TierEconomy, KiwiProvided: true, Selectable: true,
		Source: "discovered", FirstSeenAt: now, LastSeenAt: now,
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	got, err := s.ResolveModel(ctx, "o1", "kimi-k2")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got.Provider != "openrouter" {
		t.Errorf("Provider = %q, want openrouter", got.Provider)
	}
	if got.Tier != TierEconomy {
		t.Errorf("Tier = %q, want %q", got.Tier, TierEconomy)
	}
	if !got.KiwiProvided {
		t.Error("KiwiProvided = false, want true")
	}
	if got.Source != SourceCatalog {
		t.Errorf("Source = %q, want %q", got.Source, SourceCatalog)
	}
}

// An org's own row wins over the global one for the same model id.
func TestResolveModelPrefersOrgRow(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC()

	for _, m := range []*CatalogModel{
		{OrgID: GlobalCatalogOrg, ModelID: "shared-id", Provider: "openrouter",
			Tier: TierEconomy, Source: "discovered", FirstSeenAt: now, LastSeenAt: now},
		{OrgID: "o1", ModelID: "shared-id", Provider: "openai",
			Tier: TierFrontier, Source: "discovered", FirstSeenAt: now, LastSeenAt: now},
	} {
		if err := s.UpsertCatalogModel(ctx, m); err != nil {
			t.Fatalf("upsert: %v", err)
		}
	}

	got, err := s.ResolveModel(ctx, "o1", "shared-id")
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got.Provider != "openai" {
		t.Errorf("Provider = %q, want openai (the org row)", got.Provider)
	}
}

// The cheapest qualifying model wins, not the first discovered.
func TestCheapestKiwiFundedModelPicksLowestOutputCost(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC()

	established := now.Add(-30 * 24 * time.Hour) // well past catalogMaturityWindow — irrelevant to this test's cost-ordering assertion
	for _, m := range []*CatalogModel{
		{OrgID: GlobalCatalogOrg, ModelID: "pricier", Provider: "openrouter",
			Tier: TierEconomy, KiwiProvided: true, Selectable: true, OutputCostPerM: f64(1.80),
			Source: "discovered", FirstSeenAt: established, LastSeenAt: now},
		{OrgID: GlobalCatalogOrg, ModelID: "cheapest", Provider: "openrouter",
			Tier: TierEconomy, KiwiProvided: true, Selectable: true, OutputCostPerM: f64(0.40),
			Source: "discovered", FirstSeenAt: established, LastSeenAt: now},
		// A row with TierEconomy but a nil cost — Tier is stored, not
		// recomputed at query time, so nothing else stops a hand-built row
		// like this reaching the table. NULL sorts first in an ASC order in
		// both Postgres and SQLite, so without an explicit
		// "output_cost_per_m IS NOT NULL" filter this would win over
		// "cheapest" despite not actually being priced at all.
		{OrgID: GlobalCatalogOrg, ModelID: "unpriced", Provider: "openrouter",
			Tier: TierEconomy, KiwiProvided: true, Selectable: true, OutputCostPerM: nil,
			Source: "discovered", FirstSeenAt: now, LastSeenAt: now},
	} {
		if err := s.UpsertCatalogModel(ctx, m); err != nil {
			t.Fatalf("upsert: %v", err)
		}
	}

	got, ok, err := s.CheapestKiwiFundedModel(ctx, GlobalCatalogOrg, "openrouter", TierEconomy)
	if err != nil {
		t.Fatalf("CheapestKiwiFundedModel: %v", err)
	}
	if !ok || got != "cheapest" {
		t.Fatalf("got %q, ok=%v, want %q", got, ok, "cheapest")
	}
}

// Each disqualifying condition must be able to exclude a model on its own —
// this is the same "the org has a working default" guarantee requireEntitlement
// makes for the Architect default, applied to which candidates are even offered.
func TestCheapestKiwiFundedModelExcludesDisqualified(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC()

	base := func(id string) *CatalogModel {
		return &CatalogModel{
			OrgID: GlobalCatalogOrg, ModelID: id, Provider: "openrouter",
			Tier: TierEconomy, KiwiProvided: true, Selectable: true, OutputCostPerM: f64(0.10),
			// Well past catalogMaturityWindow, so each row here is disqualified
			// by exactly the one condition its test case sets — not also by
			// freshness, which would confound "each condition excludes on its own".
			Source: "discovered", FirstSeenAt: now.Add(-30 * 24 * time.Hour), LastSeenAt: now,
		}
	}
	notKiwi := base("not-kiwi-funded")
	notKiwi.KiwiProvided = false
	notSelectable := base("not-selectable")
	notSelectable.Selectable = false
	wrongTier := base("wrong-tier")
	wrongTier.Tier = TierFrontier
	wrongProvider := base("wrong-provider")
	wrongProvider.Provider = "openai"

	for _, m := range []*CatalogModel{notKiwi, notSelectable, wrongTier, wrongProvider} {
		if err := s.UpsertCatalogModel(ctx, m); err != nil {
			t.Fatalf("upsert %s: %v", m.ModelID, err)
		}
	}

	_, ok, err := s.CheapestKiwiFundedModel(ctx, GlobalCatalogOrg, "openrouter", TierEconomy)
	if err != nil {
		t.Fatalf("CheapestKiwiFundedModel: %v", err)
	}
	if ok {
		t.Fatal("expected no qualifying model, every candidate fails one condition")
	}
}

// A model that just appeared in the catalog is exactly the shape of the
// obscure, thinly-provisioned OpenRouter listing that kept 429ing/timing out
// in production (qwen/qwen3-coder on 2026-08-12/13, inclusionai/ling-2.6-flash
// on 2026-08-22 — a different model each time, none of them proven, all of
// them merely cheapest at that moment). CheapestKiwiFundedModel must not hand
// out a model until it has been in the catalog long enough to trust.
func TestCheapestKiwiFundedModelExcludesRecentlyDiscovered(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC()

	tooNew := &CatalogModel{
		OrgID: GlobalCatalogOrg, ModelID: "just-listed", Provider: "openrouter",
		Tier: TierEconomy, KiwiProvided: true, Selectable: true, OutputCostPerM: f64(0.05),
		Source: "discovered", FirstSeenAt: now, LastSeenAt: now,
	}
	established := &CatalogModel{
		OrgID: GlobalCatalogOrg, ModelID: "proven", Provider: "openrouter",
		Tier: TierEconomy, KiwiProvided: true, Selectable: true, OutputCostPerM: f64(0.90),
		Source: "discovered", FirstSeenAt: now.Add(-30 * 24 * time.Hour), LastSeenAt: now,
	}
	for _, m := range []*CatalogModel{tooNew, established} {
		if err := s.UpsertCatalogModel(ctx, m); err != nil {
			t.Fatalf("upsert %s: %v", m.ModelID, err)
		}
	}

	got, ok, err := s.CheapestKiwiFundedModel(ctx, GlobalCatalogOrg, "openrouter", TierEconomy)
	if err != nil {
		t.Fatalf("CheapestKiwiFundedModel: %v", err)
	}
	if !ok || got != "proven" {
		t.Fatalf("got %q, ok=%v, want %q (the cheaper \"just-listed\" model is too new to trust)", got, ok, "proven")
	}
}

// A full catalog reseed stamps every row with the same FirstSeenAt (this is
// exactly what happened in production on 2026-08-08 — see
// catalogMaturityWindow's doc comment). For catalogMaturityWindow after a
// reseed, every candidate fails the maturity filter at once. Returning
// ok=false here doesn't protect anyone — defaultWorkerModelFor's only
// fallback (DefaultWorkerModel) requires the org's own Anthropic key, which a
// free-tier org by definition does not have. Excluding an unproven model must
// never turn into excluding every model when nothing better exists.
func TestCheapestKiwiFundedModelFallsBackWhenMaturityExcludesEverything(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC()

	// Every row freshly (re)seeded together — nothing clears the maturity bar.
	for _, m := range []*CatalogModel{
		{OrgID: GlobalCatalogOrg, ModelID: "pricier-but-new", Provider: "openrouter",
			Tier: TierEconomy, KiwiProvided: true, Selectable: true, OutputCostPerM: f64(1.50),
			Source: "discovered", FirstSeenAt: now, LastSeenAt: now},
		{OrgID: GlobalCatalogOrg, ModelID: "cheapest-but-new", Provider: "openrouter",
			Tier: TierEconomy, KiwiProvided: true, Selectable: true, OutputCostPerM: f64(0.20),
			Source: "discovered", FirstSeenAt: now, LastSeenAt: now},
	} {
		if err := s.UpsertCatalogModel(ctx, m); err != nil {
			t.Fatalf("upsert %s: %v", m.ModelID, err)
		}
	}

	got, ok, err := s.CheapestKiwiFundedModel(ctx, GlobalCatalogOrg, "openrouter", TierEconomy)
	if err != nil {
		t.Fatalf("CheapestKiwiFundedModel: %v", err)
	}
	if !ok || got != "cheapest-but-new" {
		t.Fatalf("got %q, ok=%v, want %q (falls back to cost-only ranking when maturity excludes every candidate)", got, ok, "cheapest-but-new")
	}
}

// A miss falls back to prefix inference so existing submits keep working.
func TestResolveModelFallsBackToInference(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	cases := map[string]string{
		"gemini-2.0-flash":  "gemini",
		"gpt-5-mini":        "openai",
		"claude-opus-4-8":   "anthropic",
		"something-unknown": "anthropic",
	}
	for model, wantProvider := range cases {
		got, err := s.ResolveModel(ctx, "o1", model)
		if err != nil {
			t.Fatalf("resolve %s: %v", model, err)
		}
		if got.Provider != wantProvider {
			t.Errorf("ResolveModel(%q).Provider = %q, want %q", model, got.Provider, wantProvider)
		}
		if got.Source != SourceInferred {
			t.Errorf("ResolveModel(%q).Source = %q, want %q", model, got.Source, SourceInferred)
		}
		// Inference cannot price a model, so it can never authorise Kiwi spend.
		if got.KiwiProvided {
			t.Errorf("ResolveModel(%q) inferred a Kiwi-funded model", model)
		}
		if got.Tier != TierUnknown {
			t.Errorf("ResolveModel(%q).Tier = %q, want %q", model, got.Tier, TierUnknown)
		}
	}
}
