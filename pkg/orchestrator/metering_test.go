package orchestrator

import (
	"context"
	"testing"

	"github.com/ibreakthecloud/kiwi/pkg/store"
)

func TestMeterKiwiUsageDrawsDownTheRightTier(t *testing.T) {
	s := newTestServer(t)
	ctx := context.Background()
	seedFreeOrg(t, s, "o1")
	seedKiwiModel(t, s, "kimi-k2", "openrouter", store.TierEconomy)

	task := &store.QueuedTask{
		ID: "t1", OrgID: "o1", JobID: "j1", Funding: store.FundingKiwi,
		Spec: map[string]interface{}{"model": "kimi-k2", "task": "x"},
	}
	s.meterKiwiUsage(ctx, task, 8000, 2000, 0, 0)

	grants, err := s.storage.ListGrants(ctx, "o1", store.CurrentPeriod(timeNow()))
	if err != nil {
		t.Fatalf("ListGrants: %v", err)
	}
	var used int64
	for _, g := range grants {
		if g.Tier == store.TierEconomy {
			used = g.TokensUsed
		}
		if g.Tier == store.TierFrontier && g.TokensUsed != 0 {
			t.Errorf("frontier usage = %d; the wrong tier was charged", g.TokensUsed)
		}
	}
	// Input and output both count against the allowance.
	if used != 10000 {
		t.Errorf("economy TokensUsed = %d, want 10000", used)
	}
}

// BYOK work is the customer's own spend and must never touch a Kiwi grant.
func TestMeterKiwiUsageIgnoresBYOKTasks(t *testing.T) {
	s := newTestServer(t)
	ctx := context.Background()
	seedFreeOrg(t, s, "o1")
	seedKiwiModel(t, s, "kimi-k2", "openrouter", store.TierEconomy)

	task := &store.QueuedTask{
		ID: "t1", OrgID: "o1", JobID: "j1", Funding: store.FundingBYOK,
		Spec: map[string]interface{}{"model": "kimi-k2", "task": "x"},
	}
	s.meterKiwiUsage(ctx, task, 8000, 2000, 0, 0)

	grants, _ := s.storage.ListGrants(ctx, "o1", store.CurrentPeriod(timeNow()))
	for _, g := range grants {
		if g.TokensUsed != 0 {
			t.Errorf("tier %s charged %d tokens for BYOK work", g.Tier, g.TokensUsed)
		}
	}
}

func TestMeterKiwiUsageIgnoresZeroUsage(t *testing.T) {
	s := newTestServer(t)
	ctx := context.Background()
	seedFreeOrg(t, s, "o1")
	seedKiwiModel(t, s, "kimi-k2", "openrouter", store.TierEconomy)

	task := &store.QueuedTask{
		ID: "t1", OrgID: "o1", JobID: "j1", Funding: store.FundingKiwi,
		Spec: map[string]interface{}{"model": "kimi-k2", "task": "x"},
	}
	s.meterKiwiUsage(ctx, task, 0, 0, 0, 0)

	grants, _ := s.storage.ListGrants(ctx, "o1", store.CurrentPeriod(timeNow()))
	for _, g := range grants {
		if g.TokensUsed != 0 {
			t.Errorf("tier %s charged %d tokens for a zero-usage task", g.Tier, g.TokensUsed)
		}
	}
}

// Session mode runs two models that routinely sit in different price tiers.
// Charging the whole session to one tier would either drain a small frontier
// allowance with cheap implementer work, or bill frontier work at economy
// rates. The Architect's calls arrive tagged as the "critic" phase, so each
// role draws on its own bucket.
func TestMeterKiwiUsageSplitsSessionRolesAcrossTiers(t *testing.T) {
	s := newTestServer(t)
	ctx := context.Background()
	seedFreeOrg(t, s, "o1")

	task := &store.QueuedTask{
		ID: "t1", OrgID: "o1", JobID: "j1", Funding: store.FundingKiwi,
		Spec: map[string]interface{}{
			"model": "kimi-k2", "tier": store.TierEconomy,
			"architect_model": "big-reviewer", "architect_tier": store.TierFrontier,
		},
	}
	// 10k total, of which 3k belongs to the Architect.
	s.meterKiwiUsage(ctx, task, 8000, 2000, 2500, 500)

	used := map[string]int64{}
	grants, err := s.storage.ListGrants(ctx, "o1", store.CurrentPeriod(timeNow()))
	if err != nil {
		t.Fatalf("ListGrants: %v", err)
	}
	for _, g := range grants {
		used[g.Tier] = g.TokensUsed
	}
	if used[store.TierFrontier] != 3000 {
		t.Errorf("frontier used = %d, want 3000 (the Architect's share)", used[store.TierFrontier])
	}
	if used[store.TierEconomy] != 7000 {
		t.Errorf("economy used = %d, want 7000 (the Implementer's share)", used[store.TierEconomy])
	}
	// Nothing may be charged twice or dropped.
	if used[store.TierFrontier]+used[store.TierEconomy] != 10000 {
		t.Errorf("total charged = %d, want 10000", used[store.TierFrontier]+used[store.TierEconomy])
	}
}

// A file_loop task has one model and reports no architect tokens; its whole
// usage belongs to the one tier.
func TestMeterKiwiUsageChargesOneTierWithoutAnArchitect(t *testing.T) {
	s := newTestServer(t)
	ctx := context.Background()
	seedFreeOrg(t, s, "o1")

	task := &store.QueuedTask{
		ID: "t1", OrgID: "o1", JobID: "j1", Funding: store.FundingKiwi,
		Spec: map[string]interface{}{"model": "kimi-k2", "tier": store.TierEconomy},
	}
	s.meterKiwiUsage(ctx, task, 8000, 2000, 0, 0)

	grants, _ := s.storage.ListGrants(ctx, "o1", store.CurrentPeriod(timeNow()))
	for _, g := range grants {
		if g.Tier == store.TierEconomy && g.TokensUsed != 10000 {
			t.Errorf("economy used = %d, want 10000", g.TokensUsed)
		}
		if g.Tier == store.TierFrontier && g.TokensUsed != 0 {
			t.Errorf("frontier charged %d for a task with no Architect", g.TokensUsed)
		}
	}
}

// A Kiwi-funded Architect on a different provider from the Implementer must
// receive its own platform key, and each role is gated on its own merits.
func TestPlatformCredsCoverBothSessionModels(t *testing.T) {
	t.Setenv("KIWI_PLATFORM_OPENROUTER_API_KEY", "sk-or-platform")
	t.Setenv("KIWI_PLATFORM_ANTHROPIC_API_KEY", "sk-ant-platform")

	s := newTestServer(t)
	ctx := context.Background()
	seedFreeOrg(t, s, "o1")
	seedKiwiModel(t, s, "kimi-k2", "openrouter", store.TierEconomy)
	seedKiwiModel(t, s, "big-reviewer", "anthropic", store.TierFrontier)

	d := &store.Daemon{ID: "d1", OrgID: "o1", FleetID: store.SharedFreeFleet}
	got := s.platformCredsFor(ctx, d, "kimi-k2", "big-reviewer")

	if got["OPENROUTER_API_KEY"] != "sk-or-platform" {
		t.Errorf("the Implementer's provider key was not sealed: %v", keysOf(got))
	}
	if got["ANTHROPIC_API_KEY"] != "sk-ant-platform" {
		t.Errorf("the Architect's provider key was not sealed: %v", keysOf(got))
	}
	if len(got) != 2 {
		t.Errorf("got %d keys, want exactly 2: %v", len(got), keysOf(got))
	}
}

// The fleet gate still applies to both models. A BYOC daemon gets neither.
func TestPlatformCredsForBothModelsStillRespectTheFleetGate(t *testing.T) {
	t.Setenv("KIWI_PLATFORM_OPENROUTER_API_KEY", "sk-or-platform")
	t.Setenv("KIWI_PLATFORM_ANTHROPIC_API_KEY", "sk-ant-platform")

	s := newTestServer(t)
	ctx := context.Background()
	seedFreeOrg(t, s, "o1")
	seedKiwiModel(t, s, "kimi-k2", "openrouter", store.TierEconomy)
	seedKiwiModel(t, s, "big-reviewer", "anthropic", store.TierFrontier)

	byoc, err := s.storage.CreateFleet(ctx, "o1", "customer-cloud", store.FleetBYOC)
	if err != nil {
		t.Fatalf("create byoc fleet: %v", err)
	}
	d := &store.Daemon{ID: "d1", OrgID: "o1", FleetID: byoc.ID}
	if got := s.platformCredsFor(ctx, d, "kimi-k2", "big-reviewer"); len(got) != 0 {
		t.Fatalf("platform creds handed to a BYOC daemon: %v", keysOf(got))
	}
}
