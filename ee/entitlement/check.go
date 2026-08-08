// SPDX-License-Identifier: LicenseRef-Kiwi-BSL-1.1
// Copyright (c) 2026 RunKiwi. Licensed under the Business Source License 1.1.
// See ee/LICENSE. This is Control Plane code and is NOT Apache-2.0.

package entitlement

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/ibreakthecloud/kiwi/pkg/store"
)

var (
	// ErrExhausted reports that an org has spent its allowance for a tier.
	ErrExhausted = errors.New("token allowance exhausted")
	// ErrNotGrantable reports that a tier can never be funded by Kiwi. It is
	// returned for store.TierUnknown, which means the model could not be
	// priced — and Kiwi does not spend money it cannot count.
	ErrNotGrantable = errors.New("tier is not grantable")
)

// Store is the slice of persistence the checker needs.
type Store interface {
	EnsureGrant(ctx context.Context, orgID, tier, period string, granted int64) (*store.OrgTokenGrant, error)
	ConsumeTokens(ctx context.Context, orgID, tier, period string, n int64) error
}

// Checker answers whether an org may spend Kiwi's money, and records that it did.
type Checker struct {
	Store Store
	// Now is injectable so tests can drive period rollover.
	Now func() time.Time
}

func (c *Checker) period() string {
	if c.Now != nil {
		return store.CurrentPeriod(c.Now())
	}
	return store.CurrentPeriod(time.Now())
}

// grantable reports whether a tier can draw from an allowance at all.
func grantable(tier string) bool {
	return tier == store.TierFree || tier == store.TierEconomy || tier == store.TierFrontier
}

// Allow reports whether an org has allowance left for a tier this period,
// seeding the grant from the plan if this is its first use.
func (c *Checker) Allow(ctx context.Context, orgID, plan, tier string) (bool, error) {
	if !grantable(tier) {
		return false, fmt.Errorf("%w: %q", ErrNotGrantable, tier)
	}
	g, err := c.Store.EnsureGrant(ctx, orgID, tier, c.period(), TokensFor(plan, tier))
	if err != nil {
		return false, err
	}
	return !g.Exhausted(), nil
}

// Consume records tokens spent on a Kiwi key. It seeds the grant first so a
// task that somehow reached metering without an admission check still lands on
// a real row rather than silently going unrecorded.
func (c *Checker) Consume(ctx context.Context, orgID, plan, tier string, tokens int64) error {
	if !grantable(tier) || tokens <= 0 {
		return nil
	}
	period := c.period()
	if _, err := c.Store.EnsureGrant(ctx, orgID, tier, period, TokensFor(plan, tier)); err != nil {
		return err
	}
	return c.Store.ConsumeTokens(ctx, orgID, tier, period, tokens)
}
