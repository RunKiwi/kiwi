package agent

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/ibreakthecloud/kiwi/pkg/checkpoint"
	"github.com/ibreakthecloud/kiwi/pkg/store"
	"gorm.io/gorm"
)

// fakeProvider returns a canned output, errors when the task is "err", and
// panics when the task is "panic" — to exercise crash isolation.
type fakeProvider struct{}

func (fakeProvider) GetCodeEdit(ctx context.Context, task, fileName, codeContent, buildOutput string) (string, error) {
	switch task {
	case "panic":
		panic("boom")
	case "err":
		return "", errors.New("subtask failed")
	default:
		return "edit for " + task, nil
	}
}

func newReporter(t *testing.T, jobID string) (*StoreReporter, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if sqlDB, err := db.DB(); err == nil {
		sqlDB.SetMaxOpenConns(1)
	}
	if err := db.AutoMigrate(&store.Job{}, &store.Agent{}, &store.Event{}, &store.Checkpoint{}, &store.SideEffect{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	st := store.NewPostgresStore(db)
	return NewStoreReporter(st, checkpoint.NewService(st, checkpoint.NewLocalSnapshotter(t.TempDir())), jobID), db
}

// #35 exit: a multi-agent run completes; per-agent events recorded.
func TestMasterCoordinatesWorkers(t *testing.T) {
	const jobID = "job-1"
	rep, db := newReporter(t, jobID)
	m := &Master{JobID: jobID, Model: "claude-opus-4-8", Provider: fakeProvider{}, Reporter: rep}

	specs := []WorkerSpec{
		{ID: jobID + "-w0", Model: "claude-opus-4-8", Task: "fix a", File: "a.go"},
		{ID: jobID + "-w1", Model: "claude-opus-4-8", Task: "fix b", File: "b.go"},
	}
	res, err := m.Run(context.Background(), specs)
	if err != nil {
		t.Fatalf("master run: %v", err)
	}
	if res.Succeeded != 2 || res.Failed != 0 || res.Status != "SUCCEEDED" {
		t.Fatalf("result = %+v, want 2 succeeded / SUCCEEDED", res)
	}

	// One master + two worker rows, all terminal-succeeded.
	var agents []store.Agent
	db.Where("job_id = ?", jobID).Find(&agents)
	if len(agents) != 3 {
		t.Fatalf("agents rows = %d, want 3 (1 master + 2 workers)", len(agents))
	}
	roles := map[string]int{}
	for _, a := range agents {
		roles[a.Role]++
		if a.Status != "SUCCEEDED" {
			t.Errorf("agent %s status = %q, want SUCCEEDED", a.ID, a.Status)
		}
	}
	if roles["master"] != 1 || roles["worker"] != 2 {
		t.Errorf("roles = %v, want 1 master / 2 workers", roles)
	}

	// Per-agent events recorded (each event carries its agent_id).
	var events []store.Event
	db.Where("job_id = ?", jobID).Order("seq asc").Find(&events)
	if len(events) < 6 { // master start/done + 2×(worker start/done)
		t.Fatalf("events = %d, want >= 6", len(events))
	}
	for i, e := range events {
		if e.Seq != int64(i+1) {
			t.Errorf("event %d seq = %d, want %d (monotonic)", i, e.Seq, i+1)
		}
		if _, ok := e.Payload["agent_id"]; !ok {
			t.Errorf("event %d missing agent_id in payload", i)
		}
	}
}

// SpecsFromManifest expands each worker block by its count (RFC §5).
func TestSpecsFromManifestCount(t *testing.T) {
	manifest := &store.Manifest{Content: map[string]interface{}{
		"workers": []interface{}{
			map[string]interface{}{"model": "m1", "task": "a", "file": "a.go", "count": float64(3)},
			map[string]interface{}{"model": "m2", "task": "b", "file": "b.go"}, // no count => 1
		},
	}}
	specs := SpecsFromManifest("job-1", manifest)
	if len(specs) != 4 {
		t.Fatalf("specs = %d, want 4 (3 + 1)", len(specs))
	}
	ids := map[string]bool{}
	for _, s := range specs {
		if ids[s.ID] {
			t.Errorf("duplicate worker ID %s", s.ID)
		}
		ids[s.ID] = true
	}
	if specs[0].Model != "m1" || specs[3].Model != "m2" {
		t.Errorf("models = %q..%q, want m1..m2", specs[0].Model, specs[3].Model)
	}
}

// MaxConcurrency bounds how many workers run simultaneously.
func TestMasterBoundedConcurrency(t *testing.T) {
	const jobID = "job-3"
	rep, _ := newReporter(t, jobID)

	var mu sync.Mutex
	var inFlight, peak int
	prov := blockingProvider{onStart: func() {
		mu.Lock()
		inFlight++
		if inFlight > peak {
			peak = inFlight
		}
		mu.Unlock()
		time.Sleep(15 * time.Millisecond)
		mu.Lock()
		inFlight--
		mu.Unlock()
	}}

	m := &Master{JobID: jobID, Model: "m", Provider: prov, Reporter: rep, MaxConcurrency: 2}
	specs := make([]WorkerSpec, 6)
	for i := range specs {
		specs[i] = WorkerSpec{ID: jobID + "-w" + itoa(i), Task: "ok", File: "f.go"}
	}
	res, err := m.Run(context.Background(), specs)
	if err != nil || res.Succeeded != 6 {
		t.Fatalf("run = %+v err=%v, want 6 succeeded", res, err)
	}
	if peak > 2 {
		t.Errorf("peak concurrency = %d, want <= 2", peak)
	}
}

type blockingProvider struct{ onStart func() }

func (b blockingProvider) GetCodeEdit(ctx context.Context, task, fileName, codeContent, buildOutput string) (string, error) {
	if b.onStart != nil {
		b.onStart()
	}
	return "ok", nil
}

func itoa(n int) string { return string(rune('0' + n)) }

// A worker crash (panic) or error is contained; the master still completes and
// reports PARTIAL, recording the failed worker.
func TestWorkerCrashIsolated(t *testing.T) {
	const jobID = "job-2"
	rep, db := newReporter(t, jobID)
	m := &Master{JobID: jobID, Model: "m", Provider: fakeProvider{}, Reporter: rep}

	specs := []WorkerSpec{
		{ID: jobID + "-w0", Task: "fix a", File: "a.go"}, // succeeds
		{ID: jobID + "-w1", Task: "panic", File: "b.go"}, // panics
		{ID: jobID + "-w2", Task: "err", File: "c.go"},   // errors
	}
	res, err := m.Run(context.Background(), specs)
	if err != nil {
		t.Fatalf("master run should not error on worker failures: %v", err)
	}
	if res.Succeeded != 1 || res.Failed != 2 || res.Status != "PARTIAL" {
		t.Fatalf("result = %+v, want 1 succeeded / 2 failed / PARTIAL", res)
	}

	// The failed workers are recorded as FAILED agents; the run as a whole
	// survived the panic.
	var failed int64
	db.Model(&store.Agent{}).Where("job_id = ? AND role = ? AND status = ?", jobID, "worker", "FAILED").Count(&failed)
	if failed != 2 {
		t.Errorf("failed worker rows = %d, want 2", failed)
	}
	var master store.Agent
	db.First(&master, "id = ?", jobID+"-master")
	if master.Status != "PARTIAL" {
		t.Errorf("master status = %q, want PARTIAL", master.Status)
	}
}
