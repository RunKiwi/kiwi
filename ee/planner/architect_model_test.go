// SPDX-License-Identifier: LicenseRef-Kiwi-BSL-1.1
// Copyright (c) 2026 RunKiwi. Licensed under the Business Source License 1.1.
// See ee/LICENSE. This is Control Plane code and is NOT Apache-2.0.

package planner

import (
	"context"
	"testing"

	"github.com/ibreakthecloud/kiwi/pkg/store"
)

// The regression this whole file exists for: an unnamed Architect used to
// inherit the Implementer's model, so the cheap-implementer/expensive-architect
// split was off for every task submitted through the dashboard.
func TestArchitectDefaultsToAFrontierModelNotTheWorker(t *testing.T) {
	s := NewService(newTestStore(t), nil, nil)
	seedOrg(t, s.store.(*store.PostgresStore), "org1")
	seedCredential(t, s.store.(*store.PostgresStore), "org1", "ANTHROPIC_API_KEY")

	res, err := s.SubmitPlan(context.Background(), PlanRequest{
		OrgID: "org1", Task: "add retries", Model: "claude-haiku-4-5-20251001",
	})
	if err != nil {
		t.Fatalf("SubmitPlan: %v", err)
	}

	var task store.QueuedTask
	if err := s.store.DB().First(&task, "id = ?", res.TaskIDs[0]).Error; err != nil {
		t.Fatal(err)
	}
	got := task.Spec["architect_model"]
	if got != DefaultArchitectModel {
		t.Errorf("architect_model = %v, want %q", got, DefaultArchitectModel)
	}
	if got == task.Spec["model"] {
		t.Error("architect and implementer resolved to the same model; the split is off again")
	}
}

// The regression this covers: every Slack-triggered submit (and the CLI's
// "-model" flag left at its documented empty default) sent Model == "" all
// the way through SubmitPlan. That skipped the per-worker entitlement check
// (which only checks non-empty model names), left architectModelFor's own
// early-out treating "no Model" as "nothing to buy an Architect split for",
// and ultimately handed the daemon's provider call an empty model id. See
// DefaultWorkerModel.
func TestSubmitPlanDefaultsAnUnsetWorkerModel(t *testing.T) {
	s := NewService(newTestStore(t), nil, nil)
	seedOrg(t, s.store.(*store.PostgresStore), "org1")
	seedCredential(t, s.store.(*store.PostgresStore), "org1", "ANTHROPIC_API_KEY")

	res, err := s.SubmitPlan(context.Background(), PlanRequest{
		OrgID: "org1", Task: "add retries",
	})
	if err != nil {
		t.Fatalf("SubmitPlan: %v", err)
	}
	var task store.QueuedTask
	if err := s.store.DB().First(&task, "id = ?", res.TaskIDs[0]).Error; err != nil {
		t.Fatal(err)
	}
	if task.Spec["model"] != DefaultWorkerModel {
		t.Errorf("model = %v, want the default %q", task.Spec["model"], DefaultWorkerModel)
	}
	if task.Spec["architect_model"] != DefaultArchitectModel {
		t.Errorf("architect_model = %v, want %q — an unset Model must still get the Architect split", task.Spec["architect_model"], DefaultArchitectModel)
	}
}

// Nothing enforces the two languages staying equal — this is a manual pin.
// If this fails, either DefaultWorkerModel or frontend/src/lib/api.ts's
// DEFAULT_WORKER_MODEL changed without the other; update both together.
func TestDefaultWorkerModelMatchesFrontendConstant(t *testing.T) {
	if DefaultWorkerModel != "claude-haiku-4-5-20251001" {
		t.Errorf("DefaultWorkerModel = %q, want the literal frontend/src/lib/api.ts's DEFAULT_WORKER_MODEL is set to", DefaultWorkerModel)
	}
}

// A Kiwi-funded catalog model is tried before the BYOK DefaultWorkerModel:
// it needs no key from the org at all, which is what makes it a working
// default for an org that connected nothing of its own — the common case
// for a Slack trigger nobody pointed at a specific model.
func TestSubmitPlanPrefersACatalogKiwiFundedModelOverTheByokDefault(t *testing.T) {
	svc, ctx := newPlannerWithKiwiModel(t)
	// newPlannerWithKiwiModel's own "kimi-k2" row carries no OutputCostPerM,
	// so CheapestKiwiFundedModel's "IS NOT NULL" filter correctly excludes
	// it as unpriced — give it a real price so it qualifies here.
	if err := svc.store.(*store.PostgresStore).DB().Model(&store.CatalogModel{}).
		Where("model_id = ?", "kimi-k2").Update("output_cost_per_m", 0.60).Error; err != nil {
		t.Fatalf("price kimi-k2: %v", err)
	}

	res, err := svc.SubmitPlan(ctx, PlanRequest{
		OrgID: "o1", FleetID: store.SharedFreeFleet,
		Task: "fix the thing", RepoURL: "https://github.com/acme/api",
		TestCmd: "go test ./...",
	})
	if err != nil {
		t.Fatalf("SubmitPlan: %v", err)
	}
	var task store.QueuedTask
	if err := svc.store.DB().First(&task, "id = ?", res.TaskIDs[0]).Error; err != nil {
		t.Fatal(err)
	}
	if task.Spec["model"] != "kimi-k2" {
		t.Errorf("model = %v, want the catalog's Kiwi-funded model (kimi-k2), not the BYOK default", task.Spec["model"])
	}
}

// Regression test: a catalog row can say KiwiProvided while Kiwi holds no
// actual OpenRouter platform key for this deployment (the key was never
// configured, or was removed after the catalog was seeded). requireEntitlement
// returns nil for that case too — "nothing to fund, the org can run this on
// its own key" — which reads as "the pick is fine" unless the caller checks
// for a platform key itself. Before the fix this handed back the unusable
// OpenRouter candidate instead of falling through to the Anthropic
// DefaultWorkerModel the org actually has a key for.
func TestSubmitPlanFallsBackWhenKiwiHoldsNoOpenRouterPlatformKey(t *testing.T) {
	svc, ctx := newPlannerWithKiwiModel(t)
	st := svc.store.(*store.PostgresStore)
	seedCredential(t, st, "o1", "ANTHROPIC_API_KEY")
	if err := st.DB().Model(&store.CatalogModel{}).
		Where("model_id = ?", "kimi-k2").Update("output_cost_per_m", 0.60).Error; err != nil {
		t.Fatalf("price kimi-k2: %v", err)
	}
	// newPlannerWithKiwiModel sets this to make kimi-k2 a real candidate;
	// un-set it here to simulate the deployment having no OpenRouter key.
	t.Setenv("KIWI_PLATFORM_OPENROUTER_API_KEY", "")

	res, err := svc.SubmitPlan(ctx, PlanRequest{
		OrgID: "o1", FleetID: store.SharedFreeFleet,
		Task: "fix the thing", RepoURL: "https://github.com/acme/api",
		TestCmd: "go test ./...",
	})
	if err != nil {
		t.Fatalf("SubmitPlan: %v", err)
	}
	var task store.QueuedTask
	if err := st.DB().First(&task, "id = ?", res.TaskIDs[0]).Error; err != nil {
		t.Fatal(err)
	}
	if task.Spec["model"] != DefaultWorkerModel {
		t.Errorf("model = %v, want the BYOK default %q since Kiwi holds no OpenRouter platform key", task.Spec["model"], DefaultWorkerModel)
	}
}

// The catalog lookup must not hand back a model the org's own allowance
// cannot actually cover — that would trade one silent failure (empty Model
// reaching the daemon) for another (a submit admitted against a budget
// that immediately refuses it). Falls back to the BYOK DefaultWorkerModel
// instead, same as when the catalog has nothing at all.
func TestSubmitPlanFallsBackWhenTheCatalogModelIsOverBudget(t *testing.T) {
	svc, ctx := newPlannerWithExhaustedAllowance(t)
	st := svc.store.(*store.PostgresStore)
	seedCredential(t, st, "o1", "ANTHROPIC_API_KEY")
	// Give kimi-k2 a real price so it qualifies as a candidate at all — this
	// test's whole point is that the exhausted allowance is what excludes
	// it, not an incidental "no price" exclusion that would pass for the
	// wrong reason.
	if err := st.DB().Model(&store.CatalogModel{}).
		Where("model_id = ?", "kimi-k2").Update("output_cost_per_m", 0.60).Error; err != nil {
		t.Fatalf("price kimi-k2: %v", err)
	}

	res, err := svc.SubmitPlan(ctx, PlanRequest{
		OrgID: "o1", FleetID: store.SharedFreeFleet,
		Task: "fix the thing", RepoURL: "https://github.com/acme/api",
		TestCmd: "go test ./...",
	})
	if err != nil {
		t.Fatalf("SubmitPlan: %v", err)
	}
	var task store.QueuedTask
	if err := st.DB().First(&task, "id = ?", res.TaskIDs[0]).Error; err != nil {
		t.Fatal(err)
	}
	if task.Spec["model"] != DefaultWorkerModel {
		t.Errorf("model = %v, want the BYOK default %q since the Kiwi-funded model's allowance is exhausted", task.Spec["model"], DefaultWorkerModel)
	}
}

// An explicit choice is never second-guessed.
func TestArchitectExplicitChoiceWins(t *testing.T) {
	s := NewService(newTestStore(t), nil, nil)
	seedOrg(t, s.store.(*store.PostgresStore), "org1")
	seedCredential(t, s.store.(*store.PostgresStore), "org1", "ANTHROPIC_API_KEY")

	res, err := s.SubmitPlan(context.Background(), PlanRequest{
		OrgID: "org1", Task: "add retries", Model: "claude-haiku-4-5-20251001",
		ArchitectModel: "claude-sonnet-5",
	})
	if err != nil {
		t.Fatalf("SubmitPlan: %v", err)
	}
	var task store.QueuedTask
	if err := s.store.DB().First(&task, "id = ?", res.TaskIDs[0]).Error; err != nil {
		t.Fatal(err)
	}
	if task.Spec["architect_model"] != "claude-sonnet-5" {
		t.Errorf("architect_model = %v, want the model the user asked for", task.Spec["architect_model"])
	}
}

// The operator override exists so a fleet can move off the compiled-in default
// without a deploy.
func TestArchitectDefaultHonoursOperatorOverride(t *testing.T) {
	s := NewService(newTestStore(t), nil, nil)
	seedOrg(t, s.store.(*store.PostgresStore), "org1")
	seedCredential(t, s.store.(*store.PostgresStore), "org1", "ANTHROPIC_API_KEY")
	t.Setenv("KIWI_ARCHITECT_MODEL", "claude-sonnet-5")

	got := s.architectModelFor(context.Background(), PlanRequest{
		OrgID: "org1", Model: "claude-haiku-4-5-20251001",
	})
	if got != "claude-sonnet-5" {
		t.Errorf("architectModelFor = %q, want the override", got)
	}
}

// The rule that keeps the default safe: SubmitPlan refuses a task whose two
// models have different payers, so a default that would create that mismatch
// must decline to apply rather than manufacture a rejected submit.
func TestArchitectDefaultDeclinesOnFundingMismatch(t *testing.T) {
	s := NewService(newTestStore(t), nil, nil)
	st := s.store.(*store.PostgresStore)
	seedCredential(t, st, "org1", "ANTHROPIC_API_KEY")

	// Make the Implementer Kiwi-provided while the default Architect is not:
	// only the Implementer is in the Kiwi catalog, and a platform key exists for
	// it, which is what fundingFor requires to call a model Kiwi-funded.
	t.Setenv("KIWI_PLATFORM_ANTHROPIC_API_KEY", "sk-ant-platform")
	seedKiwiCatalogModel(t, st, "claude-haiku-4-5-20251001", "anthropic", store.TierEconomy)

	got := s.architectModelFor(context.Background(), PlanRequest{
		OrgID: "org1", Model: "claude-haiku-4-5-20251001",
	})
	if got != "" {
		t.Errorf("architectModelFor = %q, want \"\" so the daemon falls back to the worker model", got)
	}
}

// A submit that would previously have succeeded must still succeed. This is the
// property the funding and entitlement guards exist to protect: the free tier
// runs a Kiwi-provided Implementer, the frontier default is not one, and the
// mismatch must cost the org a better Architect rather than the whole submit.
func TestArchitectDefaultNeverBreaksAnOtherwiseValidSubmit(t *testing.T) {
	svc, ctx := newPlannerWithKiwiModel(t)
	t.Setenv("KIWI_PLATFORM_ANTHROPIC_API_KEY", "sk-ant-platform")
	seedCredential(t, svc.store.(*store.PostgresStore), "o1", "GIT_TOKEN")

	if _, err := svc.SubmitPlan(ctx, PlanRequest{
		OrgID: "o1", FleetID: store.SharedFreeFleet,
		Task: "fix the thing", RepoURL: "https://github.com/acme/api",
		TestCmd: "go test ./...", Model: "kimi-k2",
	}); err != nil {
		t.Fatalf("SubmitPlan should still be admitted, got: %v", err)
	}
}

// The other way a default can break a working submit: an org running their own
// Gemini key has no reason to have connected an Anthropic one, and the key
// check that admission runs is against the ARCHITECT's provider. A default that
// ignored this would tell that org to connect a second provider before a submit
// that used to work.
func TestArchitectDefaultDeclinesWithoutAKeyForItsOwnProvider(t *testing.T) {
	s := NewService(newTestStore(t), nil, nil)
	seedCredential(t, s.store.(*store.PostgresStore), "org1", "GEMINI_API_KEY")

	got := s.architectModelFor(context.Background(), PlanRequest{
		OrgID: "org1", Model: "gemini-flash-latest",
	})
	if got != "" {
		t.Errorf("architectModelFor = %q, want \"\" — the org has no key for that provider", got)
	}
}
