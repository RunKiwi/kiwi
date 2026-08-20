// SPDX-License-Identifier: LicenseRef-Kiwi-BSL-1.1
// Copyright (c) 2026 RunKiwi. Licensed under the Business Source License 1.1.
// See ee/LICENSE. This is Control Plane code and is NOT Apache-2.0.

package orchestrator

import (
	"context"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"github.com/ibreakthecloud/kiwi/ee/auth"
	"github.com/ibreakthecloud/kiwi/pkg/store"
)

func newTestServer(t *testing.T) *Server {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(
		&auth.Organization{}, &store.Fleet{}, &store.ModelEntry{}, &store.CatalogModel{}, &store.OrgTokenGrant{},
		&store.Job{}, &store.QueuedTask{}, &store.Credential{},
		&store.SlackInstallation{}, &store.SlackChannelBinding{}, &store.SlackTriggeredTask{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return &Server{db: db, storage: store.NewPostgresStore(db)}
}

// The core guarantee: a daemon Kiwi does not operate never receives a Kiwi key.
//
// seedFreeOrg is REQUIRED here even though the test is about fleets. Without an
// organizations row the function denies at the org lookup, which sits after the
// fleet check — so every case would pass with the fleet guard deleted entirely,
// and the test would prove nothing. It was written that way and did exactly
// that. Every other precondition must be satisfiable so that the fleet is the
// only reason the answer is "no".
func TestPlatformCredsDeniedToUnmanagedDaemons(t *testing.T) {
	t.Setenv("KIWI_PLATFORM_OPENROUTER_API_KEY", "sk-or-platform")

	s := newTestServer(t)
	ctx := context.Background()
	seedFreeOrg(t, s, "o1")

	byoc, err := s.storage.CreateFleet(ctx, "o1", "customer-cloud", store.FleetBYOC)
	if err != nil {
		t.Fatalf("create byoc fleet: %v", err)
	}
	// The exfiltration case: every org is given one of these at signup, and
	// CreateFleet takes the type from a request body. A customer can point a
	// join token at it and run the daemon on hardware Kiwi has never seen.
	managed, err := s.storage.CreateFleet(ctx, "o1", "Managed (Default)", store.FleetManaged)
	if err != nil {
		t.Fatalf("create managed fleet: %v", err)
	}
	seedKiwiModel(t, s, "kimi-k2", "openrouter", store.TierEconomy)

	cases := []struct {
		name    string
		fleetID string
	}{
		{"customer-created managed fleet", managed.ID},
		{"byoc fleet", byoc.ID},
		{"no fleet", ""},
		{"dangling fleet id", "flt_does_not_exist"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := &store.Daemon{ID: "d1", OrgID: "o1", FleetID: tc.fleetID}
			got := s.platformCredsFor(ctx, d, "kimi-k2")
			if len(got) != 0 {
				t.Fatalf("platform creds handed to a %s daemon: %v", tc.name, keysOf(got))
			}
		})
	}
}

// The free fleet is the whole point of the feature and must work.
func TestPlatformCredsGrantedToSharedFreeFleet(t *testing.T) {
	t.Setenv("KIWI_PLATFORM_OPENROUTER_API_KEY", "sk-or-platform")

	s := newTestServer(t)
	ctx := context.Background()
	seedFreeOrg(t, s, "o1")
	seedKiwiModel(t, s, "kimi-k2", "openrouter", store.TierEconomy)

	d := &store.Daemon{ID: "d1", OrgID: "o1", FleetID: store.SharedFreeFleet}
	got := s.platformCredsFor(ctx, d, "kimi-k2")
	if got["OPENROUTER_API_KEY"] != "sk-or-platform" {
		t.Fatalf("free-fleet daemon did not receive the platform key: %v", keysOf(got))
	}
}

// Scoping: only the provider the leased task actually needs. Bundling every
// platform key would widen exposure for no reason.
func TestPlatformCredsScopedToTheLeasedModelsProvider(t *testing.T) {
	t.Setenv("KIWI_PLATFORM_OPENROUTER_API_KEY", "sk-or-platform")
	t.Setenv("KIWI_PLATFORM_OPENAI_API_KEY", "sk-oai-platform")

	s := newTestServer(t)
	ctx := context.Background()
	seedFreeOrg(t, s, "o1")
	seedKiwiModel(t, s, "kimi-k2", "openrouter", store.TierEconomy)

	d := &store.Daemon{ID: "d1", OrgID: "o1", FleetID: store.SharedFreeFleet}
	got := s.platformCredsFor(ctx, d, "kimi-k2")
	if len(got) != 1 {
		t.Fatalf("got %d platform creds, want exactly 1: %v", len(got), keysOf(got))
	}
	if _, leaked := got["OPENAI_API_KEY"]; leaked {
		t.Error("an unrelated provider's platform key was sealed")
	}
}

// An exhausted allowance withdraws the key even on a managed fleet.
func TestPlatformCredsDeniedWhenAllowanceExhausted(t *testing.T) {
	t.Setenv("KIWI_PLATFORM_OPENROUTER_API_KEY", "sk-or-platform")

	s := newTestServer(t)
	ctx := context.Background()
	seedFreeOrg(t, s, "o1")
	seedKiwiModel(t, s, "kimi-k2", "openrouter", store.TierEconomy)

	// Spend the whole economy allowance.
	period := store.CurrentPeriod(timeNow())
	if _, err := s.storage.EnsureGrant(ctx, "o1", store.TierEconomy, period, 1000); err != nil {
		t.Fatalf("seed grant: %v", err)
	}
	if err := s.storage.ConsumeTokens(ctx, "o1", store.TierEconomy, period, 1000); err != nil {
		t.Fatalf("consume: %v", err)
	}

	d := &store.Daemon{ID: "d1", OrgID: "o1", FleetID: store.SharedFreeFleet}
	if got := s.platformCredsFor(ctx, d, "kimi-k2"); len(got) != 0 {
		t.Fatalf("platform key sealed despite an exhausted allowance: %v", keysOf(got))
	}
}

// A BYOK model must never pull a Kiwi key along with it.
func TestPlatformCredsDeniedForNonKiwiModel(t *testing.T) {
	t.Setenv("KIWI_PLATFORM_OPENROUTER_API_KEY", "sk-or-platform")

	s := newTestServer(t)
	ctx := context.Background()
	seedFreeOrg(t, s, "o1")

	d := &store.Daemon{ID: "d1", OrgID: "o1", FleetID: store.SharedFreeFleet}
	// claude-opus-4-8 is not in the catalog, so it resolves by inference and is
	// never Kiwi-funded.
	if got := s.platformCredsFor(ctx, d, "claude-opus-4-8"); len(got) != 0 {
		t.Fatalf("platform key sealed for a BYOK model: %v", keysOf(got))
	}
}

// No configured key means no key, regardless of everything else.
func TestPlatformCredsEmptyWhenNoPlatformKeyConfigured(t *testing.T) {
	t.Setenv("KIWI_PLATFORM_OPENROUTER_API_KEY", "")

	s := newTestServer(t)
	ctx := context.Background()
	seedFreeOrg(t, s, "o1")
	seedKiwiModel(t, s, "kimi-k2", "openrouter", store.TierEconomy)

	d := &store.Daemon{ID: "d1", OrgID: "o1", FleetID: store.SharedFreeFleet}
	if got := s.platformCredsFor(ctx, d, "kimi-k2"); len(got) != 0 {
		t.Fatalf("creds returned with no platform key configured: %v", keysOf(got))
	}
}

func keysOf(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// seedKiwiModel puts a Kiwi-funded model in the global catalog.
func seedKiwiModel(t *testing.T, s *Server, modelID, providerID, tier string) {
	t.Helper()
	now := timeNow()
	if err := s.storage.UpsertCatalogModel(context.Background(), &store.CatalogModel{
		OrgID: store.GlobalCatalogOrg, ModelID: modelID, Provider: providerID,
		Tier: tier, KiwiProvided: true, Selectable: true,
		Source: "discovered", FirstSeenAt: now, LastSeenAt: now,
	}); err != nil {
		t.Fatalf("seed catalog model: %v", err)
	}
}

// seedFreeOrg creates a free-plan organization row so plan lookup succeeds.
func seedFreeOrg(t *testing.T, s *Server, orgID string) {
	t.Helper()
	if err := s.db.Create(&auth.Organization{
		ID: orgID, Name: orgID, Plan: "free", ActivationState: "active",
	}).Error; err != nil {
		t.Fatalf("seed org: %v", err)
	}
}

func timeNow() time.Time { return time.Now().UTC() }
