// Package planner decomposes a high-level task into a DAG of worker specs
// (worker-spec.json), persists it as an immutable manifest, and enqueues the
// workers onto the lease queue for BYOC daemons to execute.
package planner

import "context"

// PlanRequest is a high-level task to decompose. OrgID is set from auth, not the
// request body.
type PlanRequest struct {
	OrgID      string `json:"-"`
	Task       string `json:"task"`
	RepoURL    string `json:"repo_url"`
	Ref        string `json:"ref"`
	Model      string `json:"model"`
	MaxWorkers int    `json:"max_workers"`
}

// PlannedWorker is one node in the plan DAG.
type PlannedWorker struct {
	ID        string   `json:"id"`
	Task      string   `json:"task"`
	File      string   `json:"file"`
	Model     string   `json:"model"`
	DependsOn []string `json:"depends_on,omitempty"`
}

// Plan is the planner output: a DAG of workers.
type Plan struct {
	Summary string          `json:"summary"`
	Workers []PlannedWorker `json:"workers"`
}

// Planner decomposes a high-level task into a DAG of worker specs. It is the
// seam between the deterministic HeuristicPlanner (default/tests) and the
// frontier-model-backed LLMPlanner (production).
type Planner interface {
	Plan(ctx context.Context, req PlanRequest) (*Plan, error)
}
