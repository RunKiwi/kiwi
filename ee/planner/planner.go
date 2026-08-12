// SPDX-License-Identifier: LicenseRef-Kiwi-BSL-1.1
// Copyright (c) 2026 RunKiwi. Licensed under the Business Source License 1.1.
// See ee/LICENSE. This is Control Plane code and is NOT Apache-2.0.

// Package planner decomposes a high-level task into a DAG of worker specs
// (worker-spec.json), persists it as an immutable manifest, and enqueues the
// workers onto the lease queue for BYOC daemons to execute.
package planner

import (
	"context"
	"fmt"

	"github.com/ibreakthecloud/kiwi/pkg/store"
)

// PlanRequest is a high-level task to decompose. OrgID is set from auth, not the
// request body.
type PlanRequest struct {
	OrgID          string `json:"-"`
	UserID         string `json:"-"`
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
	// Mode is accepted and ignored. Session is the only execution loop; the
	// field survives so a client written against the two-mode API still parses
	// and submits rather than failing on an unknown key.
	Mode string `json:"mode,omitempty"`
	// ArchitectModel is the planner/reviewer. It runs on the
	// customer's key in their own daemon, like the worker — the split is by
	// capability and price, not by whose credential pays.
	ArchitectModel string `json:"architect_model,omitempty"`
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
	// ReferenceMode determines how prior job learnings are injected (""|"off"|"manual"|"auto").
	ReferenceMode string `json:"reference_mode"`
	// ReferenceJobIDs specifies the jobs to inject when ReferenceMode is "manual".
	ReferenceJobIDs []string `json:"reference_job_ids,omitempty"`
	// ResolvedLearnings holds the learnings looked up during SubmitPlan, passed to the planner.
	ResolvedLearnings []store.JobLearning `json:"-"`
}

// PlannedWorker is one node in the plan DAG.
type PlannedWorker struct {
	ID             string   `json:"id"`
	Task           string   `json:"task"`
	File           string   `json:"file"`
	Files          []string `json:"files,omitempty"`
	Model          string   `json:"model"`
	TestCmd        string   `json:"test_cmd,omitempty"`
	DependsOn      []string `json:"depends_on,omitempty"`
	Mode           string   `json:"mode,omitempty"`
	ArchitectModel string   `json:"architect_model,omitempty"`
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

// defaultMaxWorkersPerJob mirrors the fallback in LeaseNextTask, so a plan is
// never rejected by a limit the queue itself would not have enforced.
const defaultMaxWorkersPerJob = 8

// Validate rejects a plan the queue could not execute correctly. A cyclic or
// dangling dependency currently manifests as tasks that simply never become
// leasable — an undiagnosable hang — so catching it at submit time is the
// difference between a clear error and a stuck job.
func (p *Plan) Validate(maxWorkers int) error {
	if len(p.Workers) == 0 {
		return fmt.Errorf("plan has no workers")
	}
	if len(p.Workers) > maxWorkers {
		return fmt.Errorf("plan exceeds maximum workers limit: %d > %d", len(p.Workers), maxWorkers)
	}

	workerMap := make(map[string]PlannedWorker)
	for _, w := range p.Workers {
		if _, exists := workerMap[w.ID]; exists {
			return fmt.Errorf("duplicate worker id: %s", w.ID)
		}
		workerMap[w.ID] = w
	}

	adj := make(map[string][]string)
	inDegree := make(map[string]int)

	for _, w := range p.Workers {
		inDegree[w.ID] = len(w.DependsOn)
		for _, dep := range w.DependsOn {
			if _, exists := workerMap[dep]; !exists {
				return fmt.Errorf("worker %s depends on unknown worker %s", w.ID, dep)
			}
			adj[dep] = append(adj[dep], w.ID)
		}
	}

	// Kahn's algorithm for topological sort (cycle detection)
	var queue []string
	for id, deg := range inDegree {
		if deg == 0 {
			queue = append(queue, id)
		}
	}

	visited := 0
	topOrder := []string{}
	for len(queue) > 0 {
		u := queue[0]
		queue = queue[1:]
		topOrder = append(topOrder, u)
		visited++

		for _, v := range adj[u] {
			inDegree[v]--
			if inDegree[v] == 0 {
				queue = append(queue, v)
			}
		}
	}

	if visited != len(p.Workers) {
		return fmt.Errorf("plan contains a cycle in dependencies")
	}

	// Reachability matrix for overlapping files check
	// reachable[u][v] = true if there is a path from u to v
	reachable := make(map[string]map[string]bool)
	for _, w := range p.Workers {
		reachable[w.ID] = make(map[string]bool)
		reachable[w.ID][w.ID] = true
	}

	// Process in reverse topological order or just transitive closure since DAG is small.
	// Since N is small (e.g. <= 20), Floyd-Warshall or simple DFS is fine.
	// Using the topological order to build reachability efficiently:
	for i := len(topOrder) - 1; i >= 0; i-- {
		u := topOrder[i]
		for _, v := range adj[u] {
			reachable[u][v] = true
			for k := range reachable[v] {
				reachable[u][k] = true
			}
		}
	}

	// Check overlapping files
	for i := 0; i < len(p.Workers); i++ {
		w1 := p.Workers[i]
		files1 := append([]string{}, w1.Files...)
		if w1.File != "" {
			files1 = append(files1, w1.File)
		}
		for j := i + 1; j < len(p.Workers); j++ {
			w2 := p.Workers[j]
			files2 := append([]string{}, w2.Files...)
			if w2.File != "" {
				files2 = append(files2, w2.File)
			}

			hasOverlap := false
			for _, f1 := range files1 {
				for _, f2 := range files2 {
					if f1 == f2 {
						hasOverlap = true
						break
					}
				}
				if hasOverlap {
					break
				}
			}

			if hasOverlap {
				if !reachable[w1.ID][w2.ID] && !reachable[w2.ID][w1.ID] {
					return fmt.Errorf("workers %s and %s overlap on files but have no dependency path", w1.ID, w2.ID)
				}
			}
		}
	}

	return nil
}
