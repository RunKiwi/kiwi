package entitlement

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ibreakthecloud/kiwi/pkg/store"
)

type fakeGrantStore struct {
	grants   map[string]*store.OrgTokenGrant
	consumed int64
}

func newFakeGrantStore() *fakeGrantStore {
	return &fakeGrantStore{grants: map[string]*store.OrgTokenGrant{}}
}

func (f *fakeGrantStore) key(orgID, tier, period string) string {
	return orgID + "|" + tier + "|" + period
}

func (f *fakeGrantStore) EnsureGrant(_ context.Context, orgID, tier, period string, granted int64) (*store.OrgTokenGrant, error) {
	k := f.key(orgID, tier, period)
	if g, ok := f.grants[k]; ok {
		return g, nil
	}
	g := &store.OrgTokenGrant{
		OrgID: orgID, Tier: tier, Period: period,
		TokensGranted: granted, UpdatedAt: time.Now().UTC(),
	}
	f.grants[k] = g
	return g, nil
}

func (f *fakeGrantStore) ConsumeTokens(_ context.Context, orgID, tier, period string, n int64) error {
	f.consumed += n
	if g, ok := f.grants[f.key(orgID, tier, period)]; ok {
		g.TokensUsed += n
	}
	return nil
}

func TestAllowWithinGrant(t *testing.T) {
	fs := newFakeGrantStore()
	c := &Checker{Store: fs}

	ok, err := c.Allow(context.Background(), "o1", "free", store.TierEconomy)
	if err != nil {
		t.Fatalf("Allow: %v", err)
	}
	if !ok {
		t.Error("a fresh free org was denied economy tokens")
	}
}

// Zero granted means denied. This is the sentinel test: if 0 were read as
// unlimited, every Free org would get unlimited frontier tokens.
func TestZeroGrantDenies(t *testing.T) {
	fs := newFakeGrantStore()
	fs.grants[fs.key("o1", store.TierFrontier, store.CurrentPeriod(time.Now()))] = &store.OrgTokenGrant{
		OrgID: "o1", Tier: store.TierFrontier, TokensGranted: 0, TokensUsed: 0,
	}
	c := &Checker{Store: fs}

	ok, err := c.Allow(context.Background(), "o1", "free", store.TierFrontier)
	if err != nil {
		t.Fatalf("Allow: %v", err)
	}
	if ok {
		t.Fatal("a zero grant allowed spending; 0 must mean no allowance, not unlimited")
	}
}

func TestUnlimitedGrantAlwaysAllows(t *testing.T) {
	fs := newFakeGrantStore()
	fs.grants[fs.key("o1", store.TierFrontier, store.CurrentPeriod(time.Now()))] = &store.OrgTokenGrant{
		OrgID: "o1", Tier: store.TierFrontier,
		TokensGranted: store.Unlimited, TokensUsed: 999_999_999,
	}
	c := &Checker{Store: fs}

	ok, err := c.Allow(context.Background(), "o1", "enterprise", store.TierFrontier)
	if err != nil {
		t.Fatalf("Allow: %v", err)
	}
	if !ok {
		t.Error("an unlimited grant denied spending")
	}
}

func TestExhaustedGrantDenies(t *testing.T) {
	fs := newFakeGrantStore()
	fs.grants[fs.key("o1", store.TierEconomy, store.CurrentPeriod(time.Now()))] = &store.OrgTokenGrant{
		OrgID: "o1", Tier: store.TierEconomy, TokensGranted: 1000, TokensUsed: 1000,
	}
	c := &Checker{Store: fs}

	ok, err := c.Allow(context.Background(), "o1", "free", store.TierEconomy)
	if err != nil {
		t.Fatalf("Allow: %v", err)
	}
	if ok {
		t.Error("an exhausted grant allowed spending")
	}
}

// An unpriceable model has no tier to draw down, so it can never be Kiwi-funded.
func TestUnknownTierIsNeverGrantable(t *testing.T) {
	c := &Checker{Store: newFakeGrantStore()}

	ok, err := c.Allow(context.Background(), "o1", "enterprise", store.TierUnknown)
	if !errors.Is(err, ErrNotGrantable) {
		t.Errorf("err = %v, want ErrNotGrantable", err)
	}
	if ok {
		t.Error("the unknown tier was allowed even on an enterprise plan")
	}
}

func TestConsumeRecordsUsage(t *testing.T) {
	fs := newFakeGrantStore()
	c := &Checker{Store: fs}

	if err := c.Consume(context.Background(), "o1", "free", store.TierEconomy, 1234); err != nil {
		t.Fatalf("Consume: %v", err)
	}
	if fs.consumed != 1234 {
		t.Errorf("consumed = %d, want 1234", fs.consumed)
	}
}
