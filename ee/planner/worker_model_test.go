// SPDX-License-Identifier: LicenseRef-Kiwi-BSL-1.1
// Copyright (c) 2026 RunKiwi. Licensed under the Business Source License 1.1.
// See ee/LICENSE. This is Control Plane code and is NOT Apache-2.0.

package planner

import (
	"context"
	"testing"

	"github.com/ibreakthecloud/kiwi/pkg/store"
)

// modelNamingFake stands in for a planning model that names a worker model of
// its own. Real models do this constantly: the planner is never told which
// providers the org has keys for, so whatever it emits is a guess.
type modelNamingFake struct{ model string }

func (f *modelNamingFake) Complete(ctx context.Context, system, user string) (string, error) {
	return `{"summary":"s","workers":[{"id":"w1","task":"t","file":"f","model":"` + f.model + `"}]}`, nil
}

// The submitter picked the worker model. The planner does not get to overrule
// it: the model id selects the provider, and therefore which of the org's keys
// the daemon looks for. This is the regression from the reported failure — a job
// submitted with gemini-flash-latest ran as claude-3-5-sonnet and died with
// "no API key configured for the anthropic provider".
func TestRequestedWorkerModelOverridesThePlanner(t *testing.T) {
	p := NewLLMPlanner(&modelNamingFake{model: "claude-3-5-sonnet"})

	plan, err := p.Plan(context.Background(), PlanRequest{
		Task:  "add a cookie consent popup",
		Model: "gemini-flash-latest",
	})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if len(plan.Workers) != 1 {
		t.Fatalf("expected 1 worker, got %d", len(plan.Workers))
	}
	if got := plan.Workers[0].Model; got != "gemini-flash-latest" {
		t.Errorf("worker model: got %q, want the requested %q — the planner must not overrule the submitter's choice", got, "gemini-flash-latest")
	}
}

// The complement: when the request names no model there is nothing to protect,
// so a model the planner supplied is still better than none.
func TestPlannerModelSurvivesWhenNoneWasRequested(t *testing.T) {
	p := NewLLMPlanner(&modelNamingFake{model: "claude-3-5-sonnet"})

	plan, err := p.Plan(context.Background(), PlanRequest{Task: "do a thing"})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if got := plan.Workers[0].Model; got != "claude-3-5-sonnet" {
		t.Errorf("worker model: got %q, want the planner's %q when the request named none", got, "claude-3-5-sonnet")
	}
}

// The user-visible path: what actually reaches the queue is the task spec, and
// that is what the daemon routes on. Asserting on the Plan alone would not prove
// the selection survives persistence.
func TestQueuedTaskCarriesTheRequestedWorkerModel(t *testing.T) {
	s := newTestStore(t)
	svc := NewService(s, nil, nil)

	t.Setenv("KIWI_PLANNER", "llm")
	t.Setenv("KIWI_PLANNER_API_KEY", "")
	if err := s.SaveCredential(context.Background(), "org-1", "GEMINI_API_KEY", store.CredentialLLM, "AIza-x"); err != nil {
		t.Fatal(err)
	}

	// Override only how the Completer is built, so the live path — key
	// resolution, provider routing, worker assembly — actually runs.
	svc.newCompleter = func(string) Completer { return &modelNamingFake{model: "claude-3-5-sonnet"} }

	seedCredential(t, s, "org-1", "GEMINI_API_KEY")

	res, err := svc.SubmitPlan(context.Background(), PlanRequest{
		OrgID:        "org-1",
		Task:         "add a cookie consent popup",
		Model:        "gemini-flash-latest",
		PlannerModel: "gemini-flash-latest",
	})
	if err != nil {
		t.Fatalf("SubmitPlan: %v", err)
	}

	var tasks []store.QueuedTask
	if err := s.DB().Where("job_id = ?", res.JobID).Find(&tasks).Error; err != nil {
		t.Fatalf("load tasks: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("expected 1 queued task, got %d", len(tasks))
	}
	if got := tasks[0].Spec["model"]; got != "gemini-flash-latest" {
		t.Errorf("queued spec model: got %v, want %q — this is the value the daemon routes on", got, "gemini-flash-latest")
	}
}
