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
	s.meterKiwiUsage(ctx, task, 8000, 2000)

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
	s.meterKiwiUsage(ctx, task, 8000, 2000)

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
	s.meterKiwiUsage(ctx, task, 0, 0)

	grants, _ := s.storage.ListGrants(ctx, "o1", store.CurrentPeriod(timeNow()))
	for _, g := range grants {
		if g.TokensUsed != 0 {
			t.Errorf("tier %s charged %d tokens for a zero-usage task", g.Tier, g.TokensUsed)
		}
	}
}
