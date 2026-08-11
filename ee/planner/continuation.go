// SPDX-License-Identifier: LicenseRef-Kiwi-BSL-1.1
// Copyright (c) 2026 RunKiwi. Licensed under the Business Source License 1.1.
// See ee/LICENSE. This is Control Plane code and is NOT Apache-2.0.

package planner

import (
	"context"
	"fmt"

	"github.com/ibreakthecloud/kiwi/pkg/store"
	"gorm.io/gorm"
)

// Continuing a task that has already produced a pull request.
//
// A review comment is not a new job. The repository, the model, the test
// command and the branch were all settled when the task was submitted, and the
// session that did the work still holds the Architect's whole history. What
// the comment supplies is one thing: a new objective for the next round.
//
// So a continuation is the parent's spec with a different task in it, keeping
// the parent's job id — because jobBranchName is "kiwi/"+JobID, delivery
// force-pushes to that branch, and CreatePR looks for an open pull request
// before opening one. Keeping the job id is the entire reason the existing
// pull request updates instead of a second one appearing.

// ContinuationInput describes a comment that should become the next round.
type ContinuationInput struct {
	OrgID string
	// ParentTask is the thread's most recent task — the one whose pull request
	// was commented on.
	ParentTask *store.QueuedTask
	// Instruction is the comment with any mention of Kiwi already stripped.
	Instruction string
	// CommentID is the GitHub comment that triggered this. Stored on the task
	// under a unique index so a webhook redelivery cannot buy a second round.
	CommentID int64
	// SessionID is the session to move onto the new task. Empty means the
	// parent never ran in session mode, and there is nothing to resume.
	SessionID string
}

// buildContinuationTask assembles the queued task, with no I/O, so the rules
// about what is inherited and what is replaced can be tested exhaustively.
func buildContinuationTask(parent *store.QueuedTask, instruction string, commentID int64) *store.QueuedTask {
	// Copy rather than share. Handing the parent's map through would let a
	// later write to the continuation's spec mutate the parent's stored row.
	spec := make(map[string]interface{}, len(parent.Spec)+1)
	for k, v := range parent.Spec {
		spec[k] = v
	}
	// The comment is the new objective. Everything else about how to do the
	// work was already decided, and re-deriving any of it would be a second
	// guess at a question the parent already answered.
	spec["task"] = instruction
	// Dependencies belonged to the original plan's DAG. A continuation waits
	// for nothing: every task it could have depended on has already run.
	delete(spec, "depends_on")

	root := parent.RootTaskID
	if root == "" {
		// A parent written before lineage existed is the root of its thread.
		root = parent.ID
	}
	parentID := parent.ID
	trigger := commentID

	return &store.QueuedTask{
		ID:      parent.JobID + "-c" + randHex(4),
		OrgID:   parent.OrgID,
		JobID:   parent.JobID,
		FleetID: parent.FleetID,
		Status:  store.TaskQueued,
		// Funding is inherited rather than re-resolved: the run continues on
		// the same model under the same payer, and re-resolving mid-thread
		// would let a catalog refresh silently move who pays for round three.
		Funding:          parent.Funding,
		Spec:             spec,
		ParentTaskID:     &parentID,
		RootTaskID:       root,
		Origin:           store.OriginPRComment,
		TriggerCommentID: &trigger,
	}
}

// SubmitContinuation enqueues the next round of an existing thread and moves
// its session onto the new task.
//
// Both happen in one transaction. A task enqueued without its session would
// start from round zero — the Architect's history discarded, the pull request
// rewritten from nothing — and a session moved without a task would strand the
// thread on a task that will never be leased.
func (s *Service) SubmitContinuation(ctx context.Context, in ContinuationInput) (*store.QueuedTask, error) {
	if in.OrgID == "" {
		return nil, fmt.Errorf("org id is required")
	}
	if in.ParentTask == nil {
		return nil, fmt.Errorf("a continuation needs the task it continues")
	}
	if in.Instruction == "" {
		return nil, fmt.Errorf("a continuation needs an instruction")
	}

	// Admission is the ordinary path. A continuation is a task like any other:
	// it spends the same allowance, on the same model, under the same caps, and
	// nothing about arriving via a comment earns it an exemption.
	model, _ := in.ParentTask.Spec["model"].(string)
	if model != "" {
		if err := s.requireEntitlement(ctx, in.OrgID, in.ParentTask.FleetID, model); err != nil {
			return nil, err
		}
		if err := s.requireAllowance(ctx, in.OrgID, model); err != nil {
			return nil, err
		}
	}

	task := buildContinuationTask(in.ParentTask, in.Instruction, in.CommentID)

	err := s.store.DB().WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(task).Error; err != nil {
			// The unique index on trigger_comment_id is what makes a webhook
			// redelivery harmless, so a conflict here is success arriving twice
			// rather than a failure.
			return fmt.Errorf("enqueue continuation for comment %d: %w", in.CommentID, err)
		}
		if in.SessionID == "" {
			return nil
		}
		// On the transaction, not through the store method: a separate
		// connection would not be atomic with the enqueue above, and on SQLite
		// it deadlocks against this transaction's own write lock.
		return store.ReattachSessionIn(tx, in.OrgID, in.SessionID, task.ID)
	})
	if err != nil {
		return nil, err
	}
	return task, nil
}
