// SPDX-License-Identifier: LicenseRef-Kiwi-BSL-1.1
// Copyright (c) 2026 RunKiwi. Licensed under the Business Source License 1.1.
// See ee/LICENSE. This is Control Plane code and is NOT Apache-2.0.

// Package entitlement decides whether an org may run a task on a Kiwi-owned
// API key, and records what it used.
//
// The allowance is denominated in tokens, banded by price tier, because a token
// is not a unit of cost: the same count costs two orders of magnitude more on a
// frontier model than on an economy one. Banding is what makes a token cap an
// actual bound on Kiwi's bill.
package entitlement

import "github.com/ibreakthecloud/kiwi/pkg/store"

// Grant is one tier's monthly token allowance.
type Grant struct {
	Tier   string `json:"tier"`
	Tokens int64  `json:"tokens"`
}

// planGrants is the single source of truth for what each plan gets per calendar
// month, mirroring the shape of auth.FreeLimits.
//
// Worst-case exposure at blended rates is roughly $1.15/month for a fully
// consuming Free org (was ~$1 before the Free frontier grant below went
// 50k -> 100k tokens; the extra 50k at a ~$2-3/M blended frontier rate is
// ~$0.10-0.15) and roughly $30/month for a Pro one. The Free number is the
// one that scales with signups and is the one to watch. These are expected
// to be tuned once a month of real consumption has been metered; they live
// here so that tuning is a one-line change.
var planGrants = map[string][]Grant{
	"free": {
		{Tier: store.TierFree, Tokens: 10_000_000},
		{Tier: store.TierEconomy, Tokens: 1_000_000},
		// 100_000, not the original 50_000: architectModelFor now actually
		// spends this tier for a zero-config Slack trigger's Architect (it
		// used to sit unused — a Kiwi-funded Implementer got no Architect
		// split at all, see ee/planner/architect_model.go), so this is real
		// new draw against a previously-idle allowance. Sized to keep the
		// same economy:frontier ratio (10:1) the Pro plan already runs, not
		// picked from observed usage — re-tune once real consumption is
		// metered.
		{Tier: store.TierFrontier, Tokens: 100_000},
	},
	"pro": {
		{Tier: store.TierFree, Tokens: 50_000_000},
		{Tier: store.TierEconomy, Tokens: 20_000_000},
		{Tier: store.TierFrontier, Tokens: 2_000_000},
	},
	"enterprise": {
		{Tier: store.TierFree, Tokens: store.Unlimited},
		{Tier: store.TierEconomy, Tokens: store.Unlimited},
		{Tier: store.TierFrontier, Tokens: store.Unlimited},
	},
}

// PlanGrants returns a plan's monthly allowances.
//
// An unrecognised plan gets the Free profile. Failing to the most restrictive
// plan rather than the most generous one is deliberate: a typo in a plan name
// must not become an unlimited allowance on Kiwi's key.
func PlanGrants(plan string) []Grant {
	if g, ok := planGrants[plan]; ok {
		out := make([]Grant, len(g))
		copy(out, g)
		return out
	}
	out := make([]Grant, len(planGrants["free"]))
	copy(out, planGrants["free"])
	return out
}

// TokensFor returns a plan's allowance for one tier. A tier that is not
// grantable — store.TierUnknown, meaning the model could not be priced —
// yields zero.
func TokensFor(plan, tier string) int64 {
	for _, g := range PlanGrants(plan) {
		if g.Tier == tier {
			return g.Tokens
		}
	}
	return 0
}
