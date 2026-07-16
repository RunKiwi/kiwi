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
	n := req.MaxWorkers
	if n <= 0 {
		n = 1
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
