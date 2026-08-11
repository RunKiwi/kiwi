// SPDX-License-Identifier: LicenseRef-Kiwi-BSL-1.1
// Copyright (c) 2026 RunKiwi. Licensed under the Business Source License 1.1.
// See ee/LICENSE. This is Control Plane code and is NOT Apache-2.0.

package planner

import (
	"context"
	"testing"

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
