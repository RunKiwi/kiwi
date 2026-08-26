// SPDX-License-Identifier: LicenseRef-Kiwi-BSL-1.1
// Copyright (c) 2026 RunKiwi. Licensed under the Business Source License 1.1.
// See ee/LICENSE. This is Control Plane code and is NOT Apache-2.0.

package entitlement

import (
	"testing"

	"github.com/ibreakthecloud/kiwi/pkg/store"
)

func TestPlanGrantsCoverEveryGrantableTier(t *testing.T) {
	grantable := []string{store.TierFree, store.TierEconomy, store.TierFrontier}
	for _, plan := range []string{"free", "pro", "enterprise"} {
		got := PlanGrants(plan)
		if len(got) != len(grantable) {
			t.Errorf("PlanGrants(%q) returned %d grants, want %d", plan, len(got), len(grantable))
		}
		seen := map[string]bool{}
		for _, g := range got {
			seen[g.Tier] = true
			// TierUnknown means unpriceable. Granting it would fund a model
			// whose cost cannot be computed.
			if g.Tier == store.TierUnknown {
				t.Errorf("PlanGrants(%q) granted the unknown tier", plan)
			}
		}
		for _, tier := range grantable {
			if !seen[tier] {
				t.Errorf("PlanGrants(%q) is missing tier %q", plan, tier)
			}
		}
	}
}

func TestFreePlanGrantsAreBounded(t *testing.T) {
	for _, g := range PlanGrants("free") {
		if g.Tokens == store.Unlimited {
			t.Errorf("the free plan grants unlimited %s tokens", g.Tier)
		}
		if g.Tokens < 0 {
			t.Errorf("free %s grant is %d; negative values other than Unlimited are meaningless", g.Tier, g.Tokens)
		}
	}
}

func TestEnterprisePlanIsUnlimited(t *testing.T) {
	for _, g := range PlanGrants("enterprise") {
		if g.Tokens != store.Unlimited {
			t.Errorf("enterprise %s grant = %d, want Unlimited (%d)", g.Tier, g.Tokens, store.Unlimited)
		}
	}
}

// An unrecognised plan must get the most restrictive profile, not the most
// generous one. Failing open here would be a billing hole.
func TestUnknownPlanFallsBackToFree(t *testing.T) {
	free := map[string]int64{}
	for _, g := range PlanGrants("free") {
		free[g.Tier] = g.Tokens
	}
	for _, g := range PlanGrants("some-plan-that-does-not-exist") {
		if g.Tokens != free[g.Tier] {
			t.Errorf("unknown plan %s grant = %d, want the free value %d", g.Tier, g.Tokens, free[g.Tier])
		}
	}
}

func TestTokensForTier(t *testing.T) {
	if got := TokensFor("free", store.TierFrontier); got != 100_000 {
		t.Errorf("TokensFor(free, frontier) = %d, want 100000", got)
	}
	// An ungrantable tier yields zero, never a positive default.
	if got := TokensFor("free", store.TierUnknown); got != 0 {
		t.Errorf("TokensFor(free, unknown) = %d, want 0", got)
	}
}
