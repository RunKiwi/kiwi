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
	ID    string
	Model string
	Task  string
	File  string
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
}

// Run executes the worker's subtask. A panic in the provider is recovered so one
// worker's crash never takes down the master or its siblings.
func (w *Worker) Run(ctx context.Context) (res WorkerResult) {
	res.AgentID = w.Spec.ID
	defer func() {
		if p := recover(); p != nil {
			res.Err = fmt.Errorf("worker %s panicked: %v", w.Spec.ID, p)
			_ = w.Reporter.Event(ctx, w.Spec.ID, "worker_done", "panic")
			_ = w.Reporter.FinishAgent(ctx, w.Spec.ID, "FAILED")
		}
	}()

	_ = w.Reporter.StartAgent(ctx, &store.Agent{
		ID: w.Spec.ID, JobID: w.JobID, Role: "worker", Model: w.Spec.Model, Status: "RUNNING",
	})
	_ = w.Reporter.Event(ctx, w.Spec.ID, "worker_start", "started")

	out, err := w.Provider.GetCodeEdit(ctx, w.Spec.Task, w.Spec.File, "", "")
	if err != nil {
		res.Err = err
		_ = w.Reporter.Event(ctx, w.Spec.ID, "worker_done", "error")
		_ = w.Reporter.FinishAgent(ctx, w.Spec.ID, "FAILED")
		return res
	}
	res.Output = out
	_ = w.Reporter.Event(ctx, w.Spec.ID, "worker_done", "success")
	_ = w.Reporter.FinishAgent(ctx, w.Spec.ID, "SUCCEEDED")
	return res
}

// Master coordinates a set of workers for one job.
type Master struct {
	JobID    string
	Model    string
	Provider provider.Provider
	Reporter Reporter
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
	_ = m.Reporter.Event(ctx, masterID, "master_start", fmt.Sprintf("decomposed into %d workers", len(specs)))

	results := make([]WorkerResult, len(specs))
	var wg sync.WaitGroup
	for i := range specs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			w := &Worker{Spec: specs[i], Provider: m.Provider, Reporter: m.Reporter, JobID: m.JobID}
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
	_ = m.Reporter.Event(ctx, masterID, "master_done", out.Status)
	_ = m.Reporter.FinishAgent(ctx, masterID, out.Status)
	return out, nil
}

// SpecsFromManifest builds the static worker set for a job from its manifest
// (manifest v1). It reads Content["workers"] as a list of {model,task,file};
// absent that, it derives a single worker from the top-level task/file.
func SpecsFromManifest(jobID string, manifest *store.Manifest) []WorkerSpec {
	if manifest == nil {
		return nil
	}
	if raw, ok := manifest.Content["workers"].([]interface{}); ok && len(raw) > 0 {
		specs := make([]WorkerSpec, 0, len(raw))
		for i, item := range raw {
			m, _ := item.(map[string]interface{})
			specs = append(specs, WorkerSpec{
				ID:    fmt.Sprintf("%s-w%d", jobID, i),
				Model: str(m["model"]),
				Task:  str(m["task"]),
				File:  str(m["file"]),
			})
		}
		return specs
	}
	return []WorkerSpec{{
		ID:    jobID + "-w0",
		Model: str(manifest.Content["model"]),
		Task:  str(manifest.Content["task"]),
		File:  str(manifest.Content["file"]),
	}}
}

func str(v interface{}) string {
	s, _ := v.(string)
	return s
}
