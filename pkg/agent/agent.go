// Package agent is the in-sandbox master/worker runtime (issue #35). A master
// decomposes a job into N workers (static per manifest v1), runs them
// concurrently and crash-isolated, aggregates their results, and reports a
// terminal status. Each agent (master + workers) is recorded as an `agents` row
// with per-agent events, via a Reporter so the runtime is decoupled from how it
// reaches the control plane (store directly in-process; the Agent API client
// from #34 when running remote).
package agent

import (
	"context"
	"fmt"
	"sync"

	"github.com/ibreakthecloud/kiwi/pkg/checkpoint"
	"github.com/ibreakthecloud/kiwi/pkg/provider"
	"github.com/ibreakthecloud/kiwi/pkg/store"
)

// Reporter records agent lifecycle + events. Implemented by StoreReporter
// in-process; a control-plane-client implementation can back it when the
// runtime runs inside a remote sandbox.
type Reporter interface {
	StartAgent(ctx context.Context, a *store.Agent) error
	FinishAgent(ctx context.Context, agentID, status string) error
	Event(ctx context.Context, agentID, phase, outcome string) error
}

// StoreReporter writes agents rows via the store and per-agent events via the
// checkpoint event log (agent_id rides in the event payload).
type StoreReporter struct {
	store  store.Store
	events *checkpoint.Service
	jobID  string
}

func NewStoreReporter(s store.Store, events *checkpoint.Service, jobID string) *StoreReporter {
	return &StoreReporter{store: s, events: events, jobID: jobID}
}

func (r *StoreReporter) StartAgent(ctx context.Context, a *store.Agent) error {
	return r.store.DB().WithContext(ctx).Create(a).Error
}

func (r *StoreReporter) FinishAgent(ctx context.Context, agentID, status string) error {
	return r.store.DB().WithContext(ctx).
		Model(&store.Agent{}).Where("id = ?", agentID).Update("status", status).Error
}

func (r *StoreReporter) Event(ctx context.Context, agentID, phase, outcome string) error {
	_, err := r.events.AppendEvent(ctx, r.jobID, phase, map[string]interface{}{
		"agent_id": agentID,
		"outcome":  outcome,
	})
	return err
}

// WorkerSpec is a single worker's scoped subtask (per-worker model/tools).
type WorkerSpec struct {
	ID      string
	Model   string
	Task    string
	File    string
	RepoURL string
	Ref     string
	// DependsOn lists the IDs of workers that must complete before this one
	// (the plan DAG produced by the planner).
	DependsOn []string
}

// WorkerResult is the outcome of one worker.
type WorkerResult struct {
	AgentID string
	Output  string
	Err     error
}

// Worker runs one scoped subtask via the provider, crash-isolated.
type Worker struct {
	Spec     WorkerSpec
	Provider provider.Provider
	Reporter Reporter
	JobID    string
	// Logf, if set, surfaces reporter failures (e.g. a network error when the
	// reporter is backed by the HTTP Agent API) instead of silently dropping
	// them. Optional; nil is a no-op.
	Logf func(format string, args ...interface{})
}

func (w *Worker) logf(format string, args ...interface{}) {
	if w.Logf != nil {
		w.Logf(format, args...)
	}
}

// event/finish wrap the reporter so a recording failure is surfaced, not
// swallowed — a dropped terminal status would leave the control plane blind to
// this worker's outcome.
func (w *Worker) event(ctx context.Context, phase, outcome string) {
	if err := w.Reporter.Event(ctx, w.Spec.ID, phase, outcome); err != nil {
		w.logf("worker %s: event %s/%s not recorded: %v", w.Spec.ID, phase, outcome, err)
	}
}

func (w *Worker) finish(ctx context.Context, status string) {
	if err := w.Reporter.FinishAgent(ctx, w.Spec.ID, status); err != nil {
		w.logf("worker %s: terminal status %q not recorded: %v", w.Spec.ID, status, err)
	}
}

// Run executes the worker's subtask. A panic in the provider is recovered so one
// worker's crash never takes down the master or its siblings.
func (w *Worker) Run(ctx context.Context) (res WorkerResult) {
	res.AgentID = w.Spec.ID
	defer func() {
		if p := recover(); p != nil {
			res.Err = fmt.Errorf("worker %s panicked: %v", w.Spec.ID, p)
			w.event(ctx, "worker_done", "panic")
			w.finish(ctx, "FAILED")
		}
	}()

	if err := w.Reporter.StartAgent(ctx, &store.Agent{
		ID: w.Spec.ID, JobID: w.JobID, Role: "worker", Model: w.Spec.Model, Status: "RUNNING",
	}); err != nil {
		w.logf("worker %s: StartAgent not recorded: %v", w.Spec.ID, err)
	}
	w.event(ctx, "worker_start", "started")

	out, err := w.Provider.GetCodeEdit(ctx, w.Spec.Task, w.Spec.File, "", "")
	if err != nil {
		res.Err = err
		w.event(ctx, "worker_done", "error")
		w.finish(ctx, "FAILED")
		return res
	}
	res.Output = out
	w.event(ctx, "worker_done", "success")
	w.finish(ctx, "SUCCEEDED")
	return res
}

// defaultMaxConcurrency bounds simultaneously-running workers when the master
// does not specify a limit, so a large worker set does not blast the provider
// into rate limits.
const defaultMaxConcurrency = 4

// Master coordinates a set of workers for one job.
type Master struct {
	JobID    string
	Model    string
	Provider provider.Provider
	Reporter Reporter
	// MaxConcurrency caps workers running at once; <=0 uses defaultMaxConcurrency.
	MaxConcurrency int
	// Logf, if set, surfaces reporter failures. Propagated to workers. Optional.
	Logf func(format string, args ...interface{})
}

func (m *Master) logf(format string, args ...interface{}) {
	if m.Logf != nil {
		m.Logf(format, args...)
	}
}

// MasterResult aggregates the run.
type MasterResult struct {
	MasterAgentID string
	Workers       []WorkerResult
	Succeeded     int
	Failed        int
	Status        string // SUCCEEDED | PARTIAL | FAILED
}

// Run spawns one worker per spec concurrently, waits for all, aggregates, and
// records terminal status. It returns a non-nil error only for an internal
// failure — individual worker failures are reflected in the result, not the
// error (the master is resilient to worker crashes).
func (m *Master) Run(ctx context.Context, specs []WorkerSpec) (MasterResult, error) {
	masterID := m.JobID + "-master"
	if err := m.Reporter.StartAgent(ctx, &store.Agent{
		ID: masterID, JobID: m.JobID, Role: "master", Model: m.Model, Status: "RUNNING",
	}); err != nil {
		return MasterResult{}, fmt.Errorf("start master agent: %w", err)
	}
	if err := m.Reporter.Event(ctx, masterID, "master_start", fmt.Sprintf("decomposed into %d workers", len(specs))); err != nil {
		m.logf("master %s: start event not recorded: %v", masterID, err)
	}

	// Bound concurrency so a large worker set respects provider rate limits.
	limit := m.MaxConcurrency
	if limit <= 0 {
		limit = defaultMaxConcurrency
	}
	sem := make(chan struct{}, limit)
	results := make([]WorkerResult, len(specs))
	var wg sync.WaitGroup
	for i := range specs {
		wg.Add(1)
		sem <- struct{}{} // acquire a slot (blocks once `limit` workers are in flight)
		go func(i int) {
			defer wg.Done()
			defer func() { <-sem }()
			w := &Worker{Spec: specs[i], Provider: m.Provider, Reporter: m.Reporter, JobID: m.JobID, Logf: m.Logf}
			results[i] = w.Run(ctx)
		}(i)
	}
	wg.Wait()

	out := MasterResult{MasterAgentID: masterID, Workers: results}
	for _, r := range results {
		if r.Err != nil {
			out.Failed++
		} else {
			out.Succeeded++
		}
	}
	switch {
	case out.Failed == 0:
		out.Status = "SUCCEEDED"
	case out.Succeeded == 0:
		out.Status = "FAILED"
	default:
		out.Status = "PARTIAL"
	}
	if err := m.Reporter.Event(ctx, masterID, "master_done", out.Status); err != nil {
		m.logf("master %s: done event not recorded: %v", masterID, err)
	}
	if err := m.Reporter.FinishAgent(ctx, masterID, out.Status); err != nil {
		m.logf("master %s: terminal status not recorded: %v", masterID, err)
	}
	return out, nil
}

// SpecsFromManifest builds the static worker set for a job from its manifest
// (manifest v1). It reads Content["workers"] as a list of blocks
// {provider,model,task,file,count}; each block with count N expands to N
// workers (RFC §5). Absent a workers list, it derives a single worker from the
// top-level task/file.
func SpecsFromManifest(jobID string, manifest *store.Manifest) []WorkerSpec {
	if manifest == nil {
		return nil
	}
	if raw, ok := manifest.Content["workers"].([]interface{}); ok && len(raw) > 0 {
		var specs []WorkerSpec
		idx := 0
		for _, item := range raw {
			m, _ := item.(map[string]interface{})
			count := 1
			if c, ok := toInt(m["count"]); ok && c > 0 {
				count = c
			}
			for k := 0; k < count; k++ {
				specs = append(specs, WorkerSpec{
					ID:        fmt.Sprintf("%s-w%d", jobID, idx),
					Model:     str(m["model"]),
					Task:      str(m["task"]),
					File:      str(m["file"]),
					RepoURL:   str(m["repo_url"]),
					Ref:       str(m["ref"]),
					DependsOn: strSlice(m["depends_on"]),
				})
				idx++
			}
		}
		return specs
	}
	return []WorkerSpec{{
		ID:      jobID + "-w0",
		Model:   str(manifest.Content["model"]),
		Task:    str(manifest.Content["task"]),
		File:    str(manifest.Content["file"]),
		RepoURL: str(manifest.Content["repo_url"]),
		Ref:     str(manifest.Content["ref"]),
	}}
}

func str(v interface{}) string {
	s, _ := v.(string)
	return s
}

// strSlice coerces a JSON-decoded []interface{} of strings (or a []string) to
// []string, ignoring non-string elements.
func strSlice(v interface{}) []string {
	switch xs := v.(type) {
	case []string:
		return xs
	case []interface{}:
		out := make([]string, 0, len(xs))
		for _, e := range xs {
			if s, ok := e.(string); ok {
				out = append(out, s)
			}
		}
		if len(out) == 0 {
			return nil
		}
		return out
	}
	return nil
}

// toInt coerces a JSON-decoded number (float64) or Go integer to int.
func toInt(v interface{}) (int, bool) {
	switch n := v.(type) {
	case float64:
		return int(n), true
	case int:
		return n, true
	case int64:
		return int(n), true
	}
	return 0, false
}
