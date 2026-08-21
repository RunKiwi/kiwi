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
