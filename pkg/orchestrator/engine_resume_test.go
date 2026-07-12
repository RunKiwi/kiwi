package orchestrator

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/ibreakthecloud/kiwi/pkg/checkpoint"
	"github.com/ibreakthecloud/kiwi/pkg/provider"
	"github.com/ibreakthecloud/kiwi/pkg/store"
	"gorm.io/gorm"
)

// fakeActor appends one 'x' to the current file content each call; the test
// command passes once the file reaches 2 bytes, so convergence needs two edits.
type fakeActor struct{ calls int }

func (f *fakeActor) GetCodeEdit(ctx context.Context, task, fileName, codeContent, buildOutput string) (string, error) {
	f.calls++
	return codeContent + "x", nil
}

type okCritic struct{}

func (okCritic) ReviewEdit(ctx context.Context, task, fileName, oldContent, newContent, buildOutput string) (provider.Verdict, error) {
	return provider.Verdict{Approved: true}, nil
}

func resumeTestStore(t *testing.T) store.Store {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if sqlDB, err := db.DB(); err == nil {
		sqlDB.SetMaxOpenConns(1)
	}
	if err := db.AutoMigrate(&store.Job{}, &store.Event{}, &store.Checkpoint{}, &store.SideEffect{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return store.NewPostgresStore(db)
}

// TestEngineResumeFromCheckpoint is the #37 integration exit: kill a run
// mid-loop, wipe the local sandbox, and confirm the engine restores the last
// checkpoint and finishes — WITHOUT re-running the already-completed step.
func TestEngineResumeFromCheckpoint(t *testing.T) {
	st := resumeTestStore(t)
	snapRoot := t.TempDir() // durable snapshot blobs (survive the sandbox wipe)
	sandboxDir := t.TempDir()
	target := filepath.Join(sandboxDir, "target.txt")
	if err := os.WriteFile(target, []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	// Passes once target.txt has >= 2 bytes.
	testCmd := `test "$(wc -c < target.txt | tr -d ' ')" -ge 2`
	const taskID = "task-1"

	newEngine := func(actor *fakeActor, maxSteps int) *Engine {
		e := NewEngine(actor, maxSteps)
		e.Critic = okCritic{}
		e.LogOut = io.Discard
		e.Checkpoints = checkpoint.NewService(st, checkpoint.NewLocalSnapshotter(snapRoot))
		e.Ledger = checkpoint.NewLedger(st)
		return e
	}

	// Run 1: capped at 1 iteration → fails, leaving a checkpoint at step 1.
	a1 := &fakeActor{}
	e1 := newEngine(a1, 1)
	if err := e1.RunTask(context.Background(), taskID, sandboxDir, "make it pass", target, testCmd); err == nil {
		t.Fatal("run 1 should not converge in a single step")
	}
	if a1.calls != 1 {
		t.Fatalf("run1 actor calls = %d, want 1", a1.calls)
	}
	cp, err := e1.Checkpoints.Latest(context.Background(), taskID)
	if err != nil {
		t.Fatalf("expected a checkpoint after run1: %v", err)
	}
	if step, _ := cp.State["step"].(float64); int(step) != 1 {
		t.Fatalf("checkpoint step = %v, want 1", cp.State["step"])
	}

	// Simulate a restart that loses the local sandbox entirely.
	os.RemoveAll(sandboxDir)

	// Run 2: fresh engine + fresh actor, same taskID/store/snapshot root.
	a2 := &fakeActor{}
	e2 := newEngine(a2, 5)
	if err := e2.RunTask(context.Background(), taskID, sandboxDir, "make it pass", target, testCmd); err != nil {
		t.Fatalf("run 2 (resume) should converge: %v", err)
	}
	// The headline guarantee: step 1 was restored, not re-run — so the resumed
	// actor is invoked exactly once (for step 2).
	if a2.calls != 1 {
		t.Errorf("run2 actor calls = %d, want 1 (resumed at step 2)", a2.calls)
	}
	got, err := os.ReadFile(target)
	if err != nil || string(got) != "xx" {
		t.Fatalf("target = %q err=%v, want \"xx\" (restored \"x\" + one resumed edit)", got, err)
	}

	// The event log spans both runs with a single monotonic sequence.
	evs, err := e2.Checkpoints.Events(context.Background(), taskID)
	if err != nil {
		t.Fatal(err)
	}
	if len(evs) < 4 {
		t.Fatalf("expected a cross-run event log, got %d events", len(evs))
	}
	for i, ev := range evs {
		if ev.Seq != int64(i+1) {
			t.Errorf("event %d seq = %d, want %d", i, ev.Seq, i+1)
		}
	}
}
