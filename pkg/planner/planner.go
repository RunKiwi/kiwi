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
	// File is the target file a worker edits, relative to the repo root.
	File string `json:"file"`
	// Files is an optional list of target files a worker edits.
	Files []string `json:"files,omitempty"`
	// Model is the worker model (runs on the customer's provider key).
	Model string `json:"model"`
	// PlannerModel optionally overrides the model that decomposes and verifies
	// the task. It runs on the Control Plane's own planning key, so it falls back
	// to the platform default when empty or unsupported by that key.
	PlannerModel string `json:"planner_model"`
	MaxWorkers   int    `json:"max_workers"`
	// FleetID optionally scopes the job to a fleet.
	FleetID string `json:"fleet_id"`
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
	Files     []string `json:"files,omitempty"`
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
