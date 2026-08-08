// SPDX-License-Identifier: LicenseRef-Kiwi-BSL-1.1
// Copyright (c) 2026 RunKiwi. Licensed under the Business Source License 1.1.
// See ee/LICENSE. This is Control Plane code and is NOT Apache-2.0.

package planner

import (
	"context"
	"fmt"
)

// HeuristicPlanner produces a deterministic DAG without calling an LLM: one
// analysis node, a fan-out of implementation workers that depend on it, and a
// verification node that depends on all of them. It is the default planner and
// the one used in tests; the frontier-model LLMPlanner plugs in behind the same
// Planner interface for production.
type HeuristicPlanner struct{}

func NewHeuristicPlanner() *HeuristicPlanner { return &HeuristicPlanner{} }

func (h *HeuristicPlanner) Plan(ctx context.Context, req PlanRequest) (*Plan, error) {
	if req.Task == "" {
		return nil, fmt.Errorf("task is required")
	}
	model := req.Model
	if model == "" {
		model = "sonnet"
	}
	// Fan out only when there are at least two distinct target files to split
	// across. Without disjoint scope, N workers would carry the same task and
	// the same file — duplicated work racing on one worktree, at N times the
	// cost. A single worker is the correct plan, not a degraded one.
	if len(req.Files) < 2 {
		return &Plan{
			Summary: "single worker: " + req.Task,
			Workers: []PlannedWorker{{
				ID:    "impl",
				Task:  req.Task,
				File:  req.File,
				Files: req.Files,
				Model: model,
			}},
		}, nil
	}

	n := req.MaxWorkers
	if n <= 0 {
		n = len(req.Files)
	}
	if n > len(req.Files) {
		n = len(req.Files)
	}

	workers := []PlannedWorker{{
		ID:    "analyze",
		Task:  "Analyze the codebase and plan changes for: " + req.Task,
		Model: model,
	}}

	implIDs := make([]string, 0, n)
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("impl-%d", i)
		implIDs = append(implIDs, id)
		workers = append(workers, PlannedWorker{
			ID:        id,
			Task:      req.Task,
			File:      req.Files[i],
			Files:     []string{req.Files[i]},
			Model:     model,
			DependsOn: []string{"analyze"},
		})
	}

	workers = append(workers, PlannedWorker{
		ID:        "verify",
		Task:      "Run tests and verify the changes for: " + req.Task,
		Model:     model,
		DependsOn: implIDs,
	})

	return &Plan{
		Summary: fmt.Sprintf("analyze → %d impl → verify", n),
		Workers: workers,
	}, nil
}
