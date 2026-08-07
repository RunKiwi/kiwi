package planner

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/ibreakthecloud/kiwi/pkg/auth"
	"github.com/ibreakthecloud/kiwi/pkg/store"
)

func newPlannerWithKiwiModel(t *testing.T) (*Service, context.Context) {
	s := newTestStore(t)
	ctx := context.Background()

	// Create org
	if err := s.DB().Create(&auth.Organization{
		ID: "o1", Name: "o1", Plan: "free", ActivationState: "active",
	}).Error; err != nil {
		t.Fatalf("seed org: %v", err)
	}

	// Create Kiwi model
	now := time.Now().UTC()
	if err := s.UpsertCatalogModel(ctx, &store.CatalogModel{
		OrgID: store.GlobalCatalogOrg, ModelID: "kimi-k2", Provider: "openrouter",
		Tier: store.TierEconomy, KiwiProvided: true, Selectable: true,
		Source: "discovered", FirstSeenAt: now, LastSeenAt: now,
	}); err != nil {
		t.Fatalf("seed catalog model: %v", err)
	}

	svc := NewService(s, nil, nil)
	return svc, ctx
}

func newPlannerWithExhaustedAllowance(t *testing.T) (*Service, context.Context) {
	svc, ctx := newPlannerWithKiwiModel(t)
	s := svc.store.(*store.PostgresStore)

	period := store.CurrentPeriod(time.Now().UTC())
	if _, err := s.EnsureGrant(ctx, "o1", store.TierEconomy, period, 1000); err != nil {
		t.Fatalf("seed grant: %v", err)
	}
	if err := s.ConsumeTokens(ctx, "o1", store.TierEconomy, period, 1000); err != nil {
		t.Fatalf("consume: %v", err)
	}

	return svc, ctx
}

func newPlannerWithBYOCFleet(t *testing.T) (*Service, context.Context) {
	svc, ctx := newPlannerWithKiwiModel(t)
	s := svc.store.(*store.PostgresStore)

	// Switch plan to pro, giving a non-managed fleet
	if err := s.DB().Model(&auth.Organization{}).Where("id = ?", "o1").Update("plan", "pro").Error; err != nil {
		t.Fatalf("update org plan: %v", err)
	}
	if _, err := s.CreateFleet(ctx, "o1", "customer-cloud", store.FleetBYOC); err != nil {
		t.Fatalf("create byoc fleet: %v", err)
	}

	return svc, ctx
}

func newPlannerWithOrgKey(t *testing.T, keyName, keyValue string) (*Service, context.Context) {
	s := newTestStore(t)
	ctx := context.Background()

	// Create org
	if err := s.DB().Create(&auth.Organization{
		ID: "o1", Name: "o1", Plan: "free", ActivationState: "active",
	}).Error; err != nil {
		t.Fatalf("seed org: %v", err)
	}

	if err := s.SaveCredential(ctx, "o1", keyName, store.CredentialLLM, keyValue); err != nil {
		t.Fatalf("save credential: %v", err)
	}

	svc := NewService(s, nil, nil)
	return svc, ctx
}

// Rejecting at submit rather than in the daemon is deliberate: a task that
// fails here gives an immediate, actionable error, where one that fails in the
// daemon sits in the queue and dies minutes later with a confusing reason.
func TestRequireEntitlementRejectsExhaustedAllowance(t *testing.T) {
	s, ctx := newPlannerWithExhaustedAllowance(t)

	err := s.requireEntitlement(ctx, "o1", "kimi-k2")
	if err == nil {
		t.Fatal("submit was admitted with an exhausted allowance")
	}
	// The dashboard's error mapper keys off this phrasing.
	if !strings.Contains(err.Error(), "allowance") {
		t.Errorf("error %q does not mention the allowance", err)
	}
}

func TestRequireEntitlementRejectsKiwiModelOnBYOCFleet(t *testing.T) {
	s, ctx := newPlannerWithBYOCFleet(t)

	err := s.requireEntitlement(ctx, "o1", "kimi-k2")
	if err == nil {
		t.Fatal("a Kiwi-funded model was admitted on a BYOC fleet")
	}
	if !strings.Contains(err.Error(), "managed fleet") {
		t.Errorf("error %q does not explain the fleet requirement", err)
	}
}

// A BYOK model is unaffected by entitlement entirely.
func TestRequireEntitlementIgnoresBYOKModels(t *testing.T) {
	s, ctx := newPlannerWithExhaustedAllowance(t)

	if err := s.requireEntitlement(ctx, "o1", "claude-opus-4-8"); err != nil {
		t.Fatalf("a BYOK model was blocked by the Kiwi allowance: %v", err)
	}
}

func TestResolveKeyReportsFunding(t *testing.T) {
	t.Setenv("KIWI_PLATFORM_OPENROUTER_API_KEY", "sk-or-platform")
	s, ctx := newPlannerWithKiwiModel(t)

	key, funding, err := s.resolveKey(ctx, "o1", "kimi-k2")
	if err != nil {
		t.Fatalf("resolveKey: %v", err)
	}
	if funding != "kiwi" {
		t.Errorf("funding = %q, want kiwi", funding)
	}
	if key != "sk-or-platform" {
		t.Errorf("resolveKey returned %q, want the platform key", key)
	}
}

func TestResolveKeyFallsBackToOrgKey(t *testing.T) {
	s, ctx := newPlannerWithOrgKey(t, "ANTHROPIC_API_KEY", "sk-ant-customer")

	key, funding, err := s.resolveKey(ctx, "o1", "claude-opus-4-8")
	if err != nil {
		t.Fatalf("resolveKey: %v", err)
	}
	if funding != "byok" {
		t.Errorf("funding = %q, want byok", funding)
	}
	if key != "sk-ant-customer" {
		t.Errorf("resolveKey returned %q, want the org key", key)
	}
}
