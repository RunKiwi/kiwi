// SPDX-License-Identifier: LicenseRef-Kiwi-BSL-1.1
// Copyright (c) 2026 RunKiwi. Licensed under the Business Source License 1.1.
// See ee/LICENSE. This is Control Plane code and is NOT Apache-2.0.

package planner

import (
	"context"
	"testing"

	"github.com/ibreakthecloud/kiwi/pkg/store"
)

func TestSubmitForkStartsFromTheParentsBranchWithANewJobID(t *testing.T) {
	s := newTestStore(t)
	seedAdmissibleOrg(t, s, "o1")
	svc := NewService(s, NewHeuristicPlanner(), nil)
	ctx := context.Background()

	// The parent as it would exist after an ordinary SubmitPlan: a real
	// QueuedTask row with a job id and a repo_url in its spec.
	parentID := "job_parent-w0"
	parent := &store.QueuedTask{
		ID: parentID, OrgID: "o1", JobID: "job_parent", Origin: store.OriginSubmit, RootTaskID: parentID,
		Spec: map[string]interface{}{"repo_url": "https://github.com/x/y"},
	}
	if err := s.DB().Create(parent).Error; err != nil {
		t.Fatalf("seed parent task: %v", err)
	}

	result, err := svc.SubmitFork(ctx, ForkInput{
		OrgID: "o1", UserID: "u1", ParentTask: parent, Instruction: "try the alternate approach instead",
	})
	if err != nil {
		t.Fatalf("SubmitFork: %v", err)
	}
	if result.JobID == parent.JobID {
		t.Fatal("expected a fork to get its own job id, not reuse the parent's")
	}

	var tasks []store.QueuedTask
	if err := s.DB().Where("job_id = ?", result.JobID).Find(&tasks).Error; err != nil || len(tasks) == 0 {
		t.Fatalf("expected at least one task enqueued under the new job id, got %v err=%v", tasks, err)
	}
	if tasks[0].Origin != store.OriginFork {
		t.Fatalf("expected Origin=fork, got %q", tasks[0].Origin)
	}
	if tasks[0].ParentTaskID == nil || *tasks[0].ParentTaskID != parent.ID {
		t.Fatal("expected ParentTaskID to point back at the source task")
	}
	if tasks[0].RootTaskID != tasks[0].ID {
		t.Fatal("expected a fork to start its own thread (RootTaskID == its own ID), not extend the parent's")
	}
}

// A fork must land on the same fleet as its parent. Leaving FleetID unset
// means "any fleet" (see QueuedTask.FleetID), which for an org running both
// a Kiwi-managed and a BYOC fleet could lease the fork to a fleet that
// cannot even reach the parent's repository/credentials.
func TestSubmitForkCarriesTheParentsFleetID(t *testing.T) {
	s := newTestStore(t)
	seedAdmissibleOrg(t, s, "o1")
	byoc, err := s.CreateFleet(context.Background(), "o1", "customer-cloud", store.FleetBYOC)
	if err != nil {
		t.Fatalf("create byoc fleet: %v", err)
	}
	svc := NewService(s, NewHeuristicPlanner(), nil)
	ctx := context.Background()

	parentID := "job_parent-w0"
	parent := &store.QueuedTask{
		ID: parentID, OrgID: "o1", JobID: "job_parent", Origin: store.OriginSubmit, RootTaskID: parentID,
		FleetID: byoc.ID,
		Spec:    map[string]interface{}{"repo_url": "https://github.com/x/y"},
	}
	if err := s.DB().Create(parent).Error; err != nil {
		t.Fatalf("seed parent task: %v", err)
	}

	result, err := svc.SubmitFork(ctx, ForkInput{
		OrgID: "o1", UserID: "u1", ParentTask: parent, Instruction: "try the alternate approach instead",
	})
	if err != nil {
		t.Fatalf("SubmitFork: %v", err)
	}

	var tasks []store.QueuedTask
	if err := s.DB().Where("job_id = ?", result.JobID).Find(&tasks).Error; err != nil || len(tasks) == 0 {
		t.Fatalf("expected at least one task enqueued under the new job id, got %v err=%v", tasks, err)
	}
	if tasks[0].FleetID != byoc.ID {
		t.Errorf("fleet_id = %q, want the parent's fleet %q", tasks[0].FleetID, byoc.ID)
	}
}

// A fork must keep the parent's Architect split rather than silently
// re-deriving one — the parent's was already vetted (funding match,
// entitlement, provider key) at its own submit time, and re-running that
// derivation now could land on a different model for reasons that have
// nothing to do with what the fork was asked to do.
func TestSubmitForkCarriesTheParentsArchitectModel(t *testing.T) {
	s := newTestStore(t)
	seedAdmissibleOrg(t, s, "o1")
	// nil planner: SubmitPlan falls back to the real SessionPlanner, unlike
	// HeuristicPlanner (this file's other tests) which never sets
	// ArchitectModel on the worker at all and would pass this test for the
	// wrong reason.
	svc := NewService(s, nil, nil)
	ctx := context.Background()

	parentID := "job_parent-w0"
	parent := &store.QueuedTask{
		ID: parentID, OrgID: "o1", JobID: "job_parent", Origin: store.OriginSubmit, RootTaskID: parentID,
		Spec: map[string]interface{}{
			"repo_url":        "https://github.com/x/y",
			"model":           "claude-haiku-4-5-20251001",
			"architect_model": "claude-sonnet-5",
		},
	}
	if err := s.DB().Create(parent).Error; err != nil {
		t.Fatalf("seed parent task: %v", err)
	}

	result, err := svc.SubmitFork(ctx, ForkInput{
		OrgID: "o1", UserID: "u1", ParentTask: parent, Instruction: "try the alternate approach instead",
	})
	if err != nil {
		t.Fatalf("SubmitFork: %v", err)
	}

	var tasks []store.QueuedTask
	if err := s.DB().Where("job_id = ?", result.JobID).Find(&tasks).Error; err != nil || len(tasks) == 0 {
		t.Fatalf("expected at least one task enqueued under the new job id, got %v err=%v", tasks, err)
	}
	if tasks[0].Spec["architect_model"] != "claude-sonnet-5" {
		t.Errorf("architect_model = %v, want the parent's %q", tasks[0].Spec["architect_model"], "claude-sonnet-5")
	}
}

// A parent that ran with no Architect split leaves nothing to carry forward
// — reading a missing "architect_model" key yields "", exactly the zero
// value SubmitPlan already treats as "no explicit choice, derive the usual
// default." architectModelFor's own tests already cover that derivation in
// full; this only proves SubmitFork does not somehow suppress it.
func TestSubmitForkOfAParentWithNoArchitectSplitStillGetsTheUsualDefault(t *testing.T) {
	s := newTestStore(t)
	seedAdmissibleOrg(t, s, "o1")
	// nil planner: see TestSubmitForkCarriesTheParentsArchitectModel — the
	// same reason applies here.
	svc := NewService(s, nil, nil)
	ctx := context.Background()

	parentID := "job_parent-w0"
	parent := &store.QueuedTask{
		ID: parentID, OrgID: "o1", JobID: "job_parent", Origin: store.OriginSubmit, RootTaskID: parentID,
		Spec: map[string]interface{}{
			"repo_url": "https://github.com/x/y",
			"model":    "claude-haiku-4-5-20251001",
			// no architect_model key at all, same as a task submitted
			// through the ordinary spec-building loop that never wrote one.
		},
	}
	if err := s.DB().Create(parent).Error; err != nil {
		t.Fatalf("seed parent task: %v", err)
	}

	result, err := svc.SubmitFork(ctx, ForkInput{
		OrgID: "o1", UserID: "u1", ParentTask: parent, Instruction: "try the alternate approach instead",
	})
	if err != nil {
		t.Fatalf("SubmitFork: %v", err)
	}

	var tasks []store.QueuedTask
	if err := s.DB().Where("job_id = ?", result.JobID).Find(&tasks).Error; err != nil || len(tasks) == 0 {
		t.Fatalf("expected at least one task enqueued under the new job id, got %v err=%v", tasks, err)
	}
	// seedAdmissibleOrg's ANTHROPIC_API_KEY covers both the worker model and
	// DefaultArchitectModel, so the usual default-injection path succeeds
	// exactly as it would for any other Anthropic-BYOK submit.
	if tasks[0].Spec["architect_model"] != DefaultArchitectModel {
		t.Errorf("architect_model = %v, want the usual default %q", tasks[0].Spec["architect_model"], DefaultArchitectModel)
	}
}
