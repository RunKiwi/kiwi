// SPDX-License-Identifier: LicenseRef-Kiwi-BSL-1.1
// Copyright (c) 2026 RunKiwi. Licensed under the Business Source License 1.1.
// See ee/LICENSE. This is Control Plane code and is NOT Apache-2.0.

package planner

import (
	"context"
	"testing"

	"github.com/ibreakthecloud/kiwi/pkg/store"
)

// SubmitPlan must persist the job row, not just the manifest and its tasks.
//
// Without it the planner path produced queued_tasks whose job_id referenced
// nothing, and every downstream write keyed on that id silently matched zero
// rows: CompleteTask's agent_minutes increment, the monthly compute cap that
// sums jobs.agent_minutes, and the per-job budget guard — which skips itself
// when the row is missing. Metering looked implemented and recorded nothing.
func TestSubmitPlanPersistsJobRow(t *testing.T) {
	s := newTestStore(t)
	seedAdmissibleOrg(t, s, "org-1")
	svc := NewService(s, &capturePlanner{}, nil)

	res, err := svc.SubmitPlan(context.Background(), PlanRequest{
		OrgID:   "org-1",
		UserID:  "user-1",
		Task:    "fix the thing",
		RepoURL: "https://github.com/owner/repo",
		Ref:     "main",
		TestCmd: "go test ./...",
	})
	if err != nil {
		t.Fatalf("SubmitPlan: %v", err)
	}

	var job store.Job
	if err := s.DB().First(&job, "id = ?", res.JobID).Error; err != nil {
		t.Fatalf("job row for %s: %v", res.JobID, err)
	}

	if job.OrgID != "org-1" {
		t.Errorf("org_id: got %q, want org-1", job.OrgID)
	}
	if job.UserID != "user-1" {
		t.Errorf("user_id: got %q, want user-1 — attribution is lost otherwise", job.UserID)
	}
	// The row has to be meterable: CompleteTask increments these columns by id,
	// so they must exist and start at zero rather than being absent.
	if job.AgentMinutes != 0 {
		t.Errorf("agent_minutes should start at 0, got %v", job.AgentMinutes)
	}
	if job.CostUSD != 0 {
		t.Errorf("cost_usd should start at 0, got %v", job.CostUSD)
	}
}

// A replayed submission must not create a second row or reset one that is
// already accruing cost and minutes.
func TestSubmitPlanJobRowIsIdempotent(t *testing.T) {
	s := newTestStore(t)
	seedAdmissibleOrg(t, s, "org-1")
	svc := NewService(s, &capturePlanner{}, nil)

	req := PlanRequest{
		OrgID:          "org-1",
		UserID:         "user-1",
		Task:           "fix the thing",
		RepoURL:        "https://github.com/owner/repo",
		IdempotencyKey: "key-1",
	}

	res, err := svc.SubmitPlan(context.Background(), req)
	if err != nil {
		t.Fatalf("first SubmitPlan: %v", err)
	}

	// Simulate the job having accrued before the replay arrives.
	if err := s.DB().Model(&store.Job{}).Where("id = ?", res.JobID).
		Update("agent_minutes", 12.5).Error; err != nil {
		t.Fatal(err)
	}

	// A replay either conflicts or no-ops; either way it must not wipe the row.
	_, _ = svc.SubmitPlan(context.Background(), req)

	var jobs []store.Job
	if err := s.DB().Where("org_id = ?", "org-1").Find(&jobs).Error; err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 1 {
		t.Fatalf("expected exactly 1 job row after replay, got %d", len(jobs))
	}
	if jobs[0].AgentMinutes != 12.5 {
		t.Errorf("replay clobbered accrued minutes: got %v, want 12.5", jobs[0].AgentMinutes)
	}
}
