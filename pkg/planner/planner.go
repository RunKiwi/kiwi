// Package planner decomposes a high-level task into a DAG of worker specs
// (worker-spec.json), persists it as an immutable manifest, and enqueues the
// workers onto the lease queue for BYOC daemons to execute.
package planner

import "context"

// PlanRequest is a high-level task to decompose. OrgID is set from auth, not the
// request body.
type PlanRequest struct {
	OrgID          string `json:"-"`
	IdempotencyKey string `json:"-"`
	Task           string `json:"task"`
	RepoURL        string `json:"repo_url"`
	Ref            string `json:"ref"`
	// File is the target file a worker edits, relative to the repo root. Threaded
	// onto the executable worker(s) so the daemon's loop has a definition of what
	// to change; without it the daemon has no target and falls back to a smoke run.
	File       string `json:"file"`
	Model      string `json:"model"`
	MaxWorkers int    `json:"max_workers"`
	// TestCmd is the command that defines "done" for the workers this plan
	// produces. Threaded onto every worker spec so the daemon's loop can verify
	// its work (the test is the definition of done).
	TestCmd string `json:"test_cmd"`
}

// PlannedWorker is one node in the plan DAG.
type PlannedWorker struct {
	ID        string   `json:"id"`
	Task      string   `json:"task"`
	File      string   `json:"file"`
	Model     string   `json:"model"`
	TestCmd   string   `json:"test_cmd,omitempty"`
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
