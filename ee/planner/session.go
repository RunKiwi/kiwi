// SPDX-License-Identifier: LicenseRef-Kiwi-BSL-1.1
// Copyright (c) 2026 RunKiwi. Licensed under the Business Source License 1.1.
// See ee/LICENSE. This is Control Plane code and is NOT Apache-2.0.

package planner

import (
	"context"
	"fmt"

	"github.com/ibreakthecloud/kiwi/pkg/agent"
)

// SessionPlanner produces the plan for a session-mode job: exactly one worker,
// with no LLM call and no file assignment.
//
// It is a Planner in name because it occupies that seam, but almost nothing is
// left of the job. Decomposition existed to divide a task into file-sized
// pieces small enough for a single-turn Actor to rewrite whole — and it had to
// guess those files from a repository URL, since the Control Plane has never
// seen the contents. An Implementer that can grep needs no such guess, so the
// planner stops guessing.
//
// What that removes from the Control Plane is worth stating plainly: no
// frontier-model call at submit time, and no read of the org's decrypted
// provider key. The second closes a containment gap this codebase already
// documented — in BYOC, planning on the Control Plane meant Kiwi's network
// making provider calls with a customer credential. In session mode the
// Architect *is* the planner, and it runs in the daemon.
//
// What remains on the Control Plane is everything else SubmitPlan does:
// admission control, org limits, idempotency, fleet routing, model policy, and
// the Job/Manifest/QueuedTask transaction. The planner stops being a decomposer
// and becomes an admission controller and job materializer.
type SessionPlanner struct{}

func NewSessionPlanner() *SessionPlanner { return &SessionPlanner{} }

// sessionWorkerID is the single worker every session plan produces. Fixed
// rather than generated so a task id is derivable from a job id, which the
// queue's dependency resolution and the dashboard both rely on.
const sessionWorkerID = "session"

// Plan returns the one-worker plan.
func (p *SessionPlanner) Plan(_ context.Context, req PlanRequest) (*Plan, error) {
	if req.Task == "" {
		return nil, fmt.Errorf("task is required")
	}

	worker := PlannedWorker{
		ID:             sessionWorkerID,
		Task:           req.Task,
		Model:          req.Model,
		ArchitectModel: req.ArchitectModel,
		Mode:           agent.ModeSession,
	}
	// File and Files are deliberately left empty even when the submitter
	// supplied them. A hint that the Control Plane cannot check against the
	// repository is exactly what the session mode exists to stop relying on, and
	// carrying one here would reintroduce it under a different name — the daemon
	// would resolve it, and a near-miss would send the Implementer to the wrong
	// place with an air of authority.

	// Learnings ride on the plan so they reach the Architect. They are resolved
	// on the Control Plane, which owns the vector index, and consumed in the
	// daemon, which now does the planning: without this they would be computed,
	// paid for, and dropped.
	summary := "Agentic session: one Architect plans and reviews, one Implementer works, until the change is approved."

	return &Plan{Summary: summary, Workers: []PlannedWorker{worker}}, nil
}
