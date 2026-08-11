// SPDX-License-Identifier: LicenseRef-Kiwi-BSL-1.1
// Copyright (c) 2026 RunKiwi. Licensed under the Business Source License 1.1.
// See ee/LICENSE. This is Control Plane code and is NOT Apache-2.0.

package planner

import (
	"context"
	"testing"

	"github.com/ibreakthecloud/kiwi/ee/auth"
	"github.com/ibreakthecloud/kiwi/pkg/store"
)

func parentTask() *store.QueuedTask {
	return &store.QueuedTask{
		ID:         "task_root",
		OrgID:      "org1",
		JobID:      "job_abc",
		FleetID:    "fleet_1",
		RootTaskID: "task_root",
		Origin:     store.OriginSubmit,
		Status:     store.TaskSucceeded,
		Funding:    store.FundingBYOK,
		Spec: map[string]interface{}{
			"task":     "add a health endpoint",
			"model":    "claude-sonnet-5",
			"provider": "anthropic",
			"test_cmd": "go test ./...",
			"repo_url": "https://github.com/acme/widgets",
			"ref":      "main",
			"job_id":   "job_abc",
			"mode":     "session",
		},
	}
}

// The keystone. jobBranchName is "kiwi/"+JobID, delivery force-pushes to it and
// CreatePR finds the open PR first — so keeping the job id is the whole reason
// a continuation updates the existing pull request instead of opening a second.
func TestContinuationKeepsTheParentsJobID(t *testing.T) {
	parent := parentTask()
	task := buildContinuationTask(parent, "rename the handler", 5551212)

	if task.JobID != "job_abc" {
		t.Errorf("job id = %q, want the parent's job_abc", task.JobID)
	}
	if task.Spec["job_id"] != "job_abc" {
		t.Errorf("spec job_id = %v, want job_abc", task.Spec["job_id"])
	}
	if task.ID == parent.ID {
		t.Error("a continuation needs its own task id")
	}
}

func TestContinuationCarriesLineage(t *testing.T) {
	parent := parentTask()
	task := buildContinuationTask(parent, "rename the handler", 5551212)

	if task.ParentTaskID == nil || *task.ParentTaskID != "task_root" {
		t.Errorf("parent = %v, want task_root", task.ParentTaskID)
	}
	if task.RootTaskID != "task_root" {
		t.Errorf("root = %q, want task_root", task.RootTaskID)
	}
	if task.Origin != store.OriginPRComment {
		t.Errorf("origin = %q, want %q", task.Origin, store.OriginPRComment)
	}
	if task.TriggerCommentID == nil || *task.TriggerCommentID != 5551212 {
		t.Errorf("trigger comment = %v, want 5551212", task.TriggerCommentID)
	}
}

// A continuation deep in a thread still belongs to the thread's root, not to
// the task it directly follows. Otherwise a three-comment conversation becomes
// three unrelated threads.
func TestContinuationOfAContinuationKeepsTheRoot(t *testing.T) {
	parent := parentTask()
	parent.ID = "task_second"
	parent.RootTaskID = "task_root"
	first := "task_root"
	parent.ParentTaskID = &first

	task := buildContinuationTask(parent, "one more thing", 999)

	if task.RootTaskID != "task_root" {
		t.Errorf("root = %q, want task_root", task.RootTaskID)
	}
	if task.ParentTaskID == nil || *task.ParentTaskID != "task_second" {
		t.Errorf("parent = %v, want task_second", task.ParentTaskID)
	}
}

// The comment is the new objective. Everything else about how to do the work —
// repo, ref, model, provider, how "done" is verified — is what the parent
// already established, and re-deriving any of it would be a second guess at a
// question already answered.
func TestContinuationReplacesTheTaskAndInheritsTheRest(t *testing.T) {
	parent := parentTask()
	task := buildContinuationTask(parent, "rename the handler", 1)

	if task.Spec["task"] != "rename the handler" {
		t.Errorf("task = %v, want the instruction", task.Spec["task"])
	}
	for _, key := range []string{"model", "provider", "test_cmd", "repo_url", "ref", "mode"} {
		if task.Spec[key] != parent.Spec[key] {
			t.Errorf("%s = %v, want the parent's %v", key, task.Spec[key], parent.Spec[key])
		}
	}
	if task.FleetID != parent.FleetID {
		t.Errorf("fleet = %q, want the parent's %q", task.FleetID, parent.FleetID)
	}
	if task.Funding != parent.Funding {
		t.Errorf("funding = %q, want the parent's %q", task.Funding, parent.Funding)
	}
	if task.Status != store.TaskQueued {
		t.Errorf("status = %q, want QUEUED", task.Status)
	}
}

// The spec is copied, not shared. A map handed straight through would let a
// later write to the continuation's spec mutate the parent's stored row.
func TestContinuationDoesNotShareTheParentsSpecMap(t *testing.T) {
	parent := parentTask()
	task := buildContinuationTask(parent, "rename the handler", 1)

	task.Spec["model"] = "something-else"
	if parent.Spec["model"] != "claude-sonnet-5" {
		t.Error("writing to the continuation's spec changed the parent's")
	}
}

func TestSubmitContinuationRejectsAParentWithNoSession(t *testing.T) {
	s := &Service{}
	_, err := s.SubmitContinuation(context.Background(), ContinuationInput{
		OrgID:       "org1",
		Instruction: "do the thing",
	})
	if err == nil {
		t.Error("expected an error when no parent task is given")
	}
}

// The bug this exists to prevent: a comment enqueued a continuation, the task
// appeared in the dashboard, and it sat there reporting "no runner is connected
// that can execute this task" while the fleet host was running.
//
// HandlePlan cold-starts a per-org daemon after every submit. SubmitContinuation
// is a second submit path that skipped it, so the work was queued for a fleet
// with nothing to lease it.
func TestContinuationColdStartsTheOrgsDaemon(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	if err := st.DB().Create(&auth.Organization{ID: "org1", Name: "acme", Plan: "free"}).Error; err != nil {
		t.Fatal(err)
	}
	svc := NewService(st, NewHeuristicPlanner(), nil)

	parent := parentTask()
	if err := st.EnqueueTask(ctx, parent); err != nil {
		t.Fatal(err)
	}

	if _, err := svc.SubmitContinuation(ctx, ContinuationInput{
		OrgID: "org1", ParentTask: parent, Instruction: "rename the handler", CommentID: 1,
	}); err != nil {
		t.Fatal(err)
	}

	var provisions []auth.ProvisioningRequest
	if err := st.DB().Where("org_id = ?", "org1").Find(&provisions).Error; err != nil {
		t.Fatal(err)
	}
	if len(provisions) != 1 {
		t.Fatalf("got %d provisioning requests, want 1 — the task has no runner without one", len(provisions))
	}
}

// A paid org runs on its own fleet and must not have a free daemon provisioned
// for it.
func TestContinuationDoesNotColdStartAPaidOrg(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	if err := st.DB().Create(&auth.Organization{ID: "org1", Name: "acme", Plan: "pro"}).Error; err != nil {
		t.Fatal(err)
	}
	svc := NewService(st, NewHeuristicPlanner(), nil)

	parent := parentTask()
	if err := st.EnqueueTask(ctx, parent); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SubmitContinuation(ctx, ContinuationInput{
		OrgID: "org1", ParentTask: parent, Instruction: "rename it", CommentID: 2,
	}); err != nil {
		t.Fatal(err)
	}

	var n int64
	if err := st.DB().Model(&auth.ProvisioningRequest{}).Where("org_id = ?", "org1").Count(&n).Error; err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("got %d provisioning requests for a paid org, want 0", n)
	}
}

// HandlePlan refuses a suspended org. A second submit path that does not is a
// way around an abuse suspension: comment on an old pull request and keep
// spending.
func TestContinuationRefusesASuspendedOrg(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	if err := st.DB().Create(&auth.Organization{
		ID: "org1", Name: "acme", Plan: "free", ActivationState: "suspended",
	}).Error; err != nil {
		t.Fatal(err)
	}
	svc := NewService(st, NewHeuristicPlanner(), nil)

	parent := parentTask()
	if err := st.EnqueueTask(ctx, parent); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.SubmitContinuation(ctx, ContinuationInput{
		OrgID: "org1", ParentTask: parent, Instruction: "keep going", CommentID: 3,
	}); err == nil {
		t.Fatal("a suspended org must not be able to buy work by commenting")
	}

	var n int64
	if err := st.DB().Model(&store.QueuedTask{}).Where("origin = ?", store.OriginPRComment).Count(&n).Error; err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("got %d continuations for a suspended org, want 0", n)
	}
}
