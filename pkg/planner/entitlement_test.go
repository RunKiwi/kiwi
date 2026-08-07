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
	// A catalog row saying kiwi_provided is not enough on its own: entitlement
	// only applies when Kiwi actually holds a key for the provider. Without
	// this the checks correctly admit everything and the tests below pass for
	// the wrong reason.
	t.Setenv("KIWI_PLATFORM_OPENROUTER_API_KEY", "sk-or-platform")

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

	err := s.requireEntitlement(ctx, "o1", store.SharedFreeFleet, "kimi-k2")
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

	err := s.requireEntitlement(ctx, "o1", "flt_customer_byoc", "kimi-k2")
	if err == nil {
		t.Fatal("a Kiwi-funded model was admitted on a BYOC fleet")
	}
	if !strings.Contains(err.Error(), "Kiwi-managed fleet") {
		t.Errorf("error %q does not explain the fleet requirement", err)
	}
}

// A BYOK model is unaffected by entitlement entirely.
func TestRequireEntitlementIgnoresBYOKModels(t *testing.T) {
	s, ctx := newPlannerWithExhaustedAllowance(t)

	if err := s.requireEntitlement(ctx, "o1", store.SharedFreeFleet, "claude-opus-4-8"); err != nil {
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

// A task's funding must follow the WORKER's model, not the planner's.
//
// It used to be the job-level planner funding, copied onto every task. On the
// live path — the heuristic planner, which is what runs when KIWI_PLANNER is
// unset — that value is always "byok". So a worker running on a Kiwi-funded
// model was recorded as customer-funded, meterKiwiUsage skipped it, tokens_used
// never moved, the allowance never exhausted, and the cap did not exist.
func TestTaskFundingFollowsTheWorkerModel(t *testing.T) {
	svc, ctx := newPlannerWithKiwiModel(t)
	st := svc.store.(*store.PostgresStore)

	res, err := svc.SubmitPlan(ctx, PlanRequest{
		OrgID:   "o1",
		FleetID: store.SharedFreeFleet,
		Task:    "fix the thing",
		RepoURL: "https://github.com/acme/api",
		TestCmd: "go test ./...",
		Model:   "kimi-k2",
	})
	if err != nil {
		t.Fatalf("SubmitPlan: %v", err)
	}
	if len(res.TaskIDs) == 0 {
		t.Fatal("no tasks enqueued")
	}

	var tasks []store.QueuedTask
	if err := st.DB().Where("job_id = ?", res.JobID).Find(&tasks).Error; err != nil {
		t.Fatalf("load tasks: %v", err)
	}
	for _, task := range tasks {
		model, _ := task.Spec["model"].(string)
		if model != "kimi-k2" {
			continue
		}
		if task.Funding != store.FundingKiwi {
			t.Errorf("task %s runs %s on Kiwi's key but Funding = %q; its usage will never be metered",
				task.ID, model, task.Funding)
		}
		// Pinned so the daemon can route without a catalog and metering charges
		// the tier the submit was admitted against.
		if got, _ := task.Spec["provider"].(string); got != "openrouter" {
			t.Errorf("task %s spec provider = %q, want openrouter", task.ID, got)
		}
		if got, _ := task.Spec["tier"].(string); got != store.TierEconomy {
			t.Errorf("task %s spec tier = %q, want %q", task.ID, got, store.TierEconomy)
		}
	}
}

// The same path must refuse when the allowance is gone — previously it was
// admitted, because no branch of the planner switch checked worker models.
func TestSubmitRejectsExhaustedAllowanceOnHeuristicPath(t *testing.T) {
	svc, ctx := newPlannerWithExhaustedAllowance(t)

	_, err := svc.SubmitPlan(ctx, PlanRequest{
		OrgID:   "o1",
		FleetID: store.SharedFreeFleet,
		Task:    "fix the thing",
		RepoURL: "https://github.com/acme/api",
		TestCmd: "go test ./...",
		Model:   "kimi-k2",
	})
	if err == nil {
		t.Fatal("submit admitted with an exhausted Kiwi allowance")
	}
	if !strings.Contains(err.Error(), "allowance") {
		t.Errorf("error %q does not mention the allowance", err)
	}
}
