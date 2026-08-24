package store

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestCurrentPeriodIsUTCCalendarMonth(t *testing.T) {
	// A local-time period would roll over at different instants for different
	// deployments and could grant a second allowance at a timezone boundary.
	got := CurrentPeriod(time.Date(2026, 8, 31, 23, 30, 0, 0, time.FixedZone("UTC+5", 5*3600)))
	if got != "2026-08" {
		t.Errorf("CurrentPeriod = %q, want 2026-08 (the UTC month)", got)
	}
	if got := CurrentPeriod(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)); got != "2026-01" {
		t.Errorf("CurrentPeriod = %q, want 2026-01", got)
	}
}

func TestEnsureGrantSeedsOnceAndIsIdempotent(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	g, err := s.EnsureGrant(ctx, "o1", TierEconomy, "2026-08", 1_000_000)
	if err != nil {
		t.Fatalf("EnsureGrant: %v", err)
	}
	if g.TokensGranted != 1_000_000 || g.TokensUsed != 0 {
		t.Fatalf("seeded grant = %+v", g)
	}

	if err := s.ConsumeTokens(ctx, "o1", TierEconomy, "2026-08", 400_000); err != nil {
		t.Fatalf("ConsumeTokens: %v", err)
	}

	// A second EnsureGrant must not reset usage — that would hand out a fresh
	// allowance on every task.
	g2, err := s.EnsureGrant(ctx, "o1", TierEconomy, "2026-08", 1_000_000)
	if err != nil {
		t.Fatalf("EnsureGrant (second): %v", err)
	}
	if g2.TokensUsed != 400_000 {
		t.Errorf("TokensUsed = %d after re-ensure, want 400000", g2.TokensUsed)
	}
}

// A row's TokensGranted, once created, is authoritative — an admin may have
// set it to something other than the plan default (a custom or reduced
// grant), and every admission check calls EnsureGrant with the plan's current
// default. That must not silently raise the row back to the default.
func TestEnsureGrantDoesNotOverwriteAnExistingGrant(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// Seed low (e.g. an admin-imposed reduced grant), then exhaust it.
	if _, err := s.EnsureGrant(ctx, "o1", TierEconomy, "2026-08", 1000); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := s.ConsumeTokens(ctx, "o1", TierEconomy, "2026-08", 1000); err != nil {
		t.Fatalf("consume: %v", err)
	}

	// A later admission check passes the plan's (higher) default; the existing
	// row must not be silently raised back up, un-exhausting the allowance.
	g, err := s.EnsureGrant(ctx, "o1", TierEconomy, "2026-08", 1_000_000)
	if err != nil {
		t.Fatalf("EnsureGrant: %v", err)
	}
	if g.TokensGranted != 1000 {
		t.Errorf("TokensGranted = %d, want 1000 (existing grant silently overwritten)", g.TokensGranted)
	}
	if !g.Exhausted() {
		t.Error("grant should still be exhausted after a re-check with a higher plan default")
	}
}

// A new calendar month is a fresh allowance without any scheduled job.
func TestEnsureGrantIsPerPeriod(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	if _, err := s.EnsureGrant(ctx, "o1", TierEconomy, "2026-08", 1_000_000); err != nil {
		t.Fatalf("seed aug: %v", err)
	}
	if err := s.ConsumeTokens(ctx, "o1", TierEconomy, "2026-08", 1_000_000); err != nil {
		t.Fatalf("consume aug: %v", err)
	}

	sep, err := s.EnsureGrant(ctx, "o1", TierEconomy, "2026-09", 1_000_000)
	if err != nil {
		t.Fatalf("seed sep: %v", err)
	}
	if sep.TokensUsed != 0 {
		t.Errorf("September usage = %d, want 0", sep.TokensUsed)
	}
}

// Metering runs concurrently with other tasks. A read-modify-write would lose
// updates and undercount, which is the direction that costs money.
func TestConsumeTokensIsAtomic(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	if _, err := s.EnsureGrant(ctx, "o1", TierEconomy, "2026-08", 1_000_000); err != nil {
		t.Fatalf("seed: %v", err)
	}

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = s.ConsumeTokens(ctx, "o1", TierEconomy, "2026-08", 1000)
		}()
	}
	wg.Wait()

	grants, err := s.ListGrants(ctx, "o1", "2026-08")
	if err != nil {
		t.Fatalf("ListGrants: %v", err)
	}
	if len(grants) != 1 {
		t.Fatalf("got %d grants, want 1", len(grants))
	}
	if grants[0].TokensUsed != 20_000 {
		t.Errorf("TokensUsed = %d, want 20000; concurrent metering lost updates", grants[0].TokensUsed)
	}
}

func TestListGrantsIsOrgScoped(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	if _, err := s.EnsureGrant(ctx, "o1", TierEconomy, "2026-08", 1000); err != nil {
		t.Fatalf("seed o1: %v", err)
	}
	if _, err := s.EnsureGrant(ctx, "o2", TierEconomy, "2026-08", 9999); err != nil {
		t.Fatalf("seed o2: %v", err)
	}

	got, err := s.ListGrants(ctx, "o1", "2026-08")
	if err != nil {
		t.Fatalf("ListGrants: %v", err)
	}
	if len(got) != 1 || got[0].TokensGranted != 1000 {
		t.Errorf("ListGrants(o1) = %+v; org scoping is broken", got)
	}
}
