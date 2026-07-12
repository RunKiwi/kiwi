package checkpoint

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/ibreakthecloud/kiwi/pkg/store"
	"gorm.io/gorm"
)

func newStore(t *testing.T) store.Store {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(
		&store.Organization{}, &store.OrgLimits{}, &store.Job{}, &store.Outbox{},
		&store.Event{}, &store.Checkpoint{}, &store.SideEffect{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return store.NewSQLiteStore(db)
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// #36 exit: a snapshot restores to a byte-identical workspace — verified by the
// content hash matching after restore.
func TestSnapshotRoundTrip(t *testing.T) {
	snap := NewLocalSnapshotter(t.TempDir())
	dir := t.TempDir()
	writeFile(t, dir, "a.txt", "alpha")
	if err := os.MkdirAll(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, dir, "sub/b.txt", "beta")

	uri, hash, err := snap.Snapshot(dir)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}

	// Corrupt the workspace, then restore from the blob.
	writeFile(t, dir, "a.txt", "CORRUPTED")
	os.Remove(filepath.Join(dir, "sub", "b.txt"))
	if err := snap.Restore(uri, dir); err != nil {
		t.Fatalf("restore: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(dir, "a.txt"))
	if err != nil || string(got) != "alpha" {
		t.Fatalf("a.txt = %q err=%v, want alpha", got, err)
	}
	// Re-snapshotting the restored tree must reproduce the original hash.
	_, hash2, err := snap.Snapshot(dir)
	if err != nil {
		t.Fatal(err)
	}
	if hash != hash2 {
		t.Errorf("hash drift after restore: %s != %s", hash, hash2)
	}
}

// Events get a per-job monotonic seq and read back in order.
func TestEventLogMonotonic(t *testing.T) {
	svc := NewService(newStore(t), NewLocalSnapshotter(t.TempDir()))
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		ev, err := svc.AppendEvent(ctx, "j1", "phase", map[string]interface{}{"i": i})
		if err != nil {
			t.Fatal(err)
		}
		if ev.Seq != int64(i+1) {
			t.Errorf("event %d seq = %d, want %d", i, ev.Seq, i+1)
		}
	}
	// A different job has its own independent sequence.
	ev, _ := svc.AppendEvent(ctx, "j2", "phase", nil)
	if ev.Seq != 1 {
		t.Errorf("job j2 first seq = %d, want 1", ev.Seq)
	}
	evs, err := svc.Events(ctx, "j1")
	if err != nil || len(evs) != 3 || evs[0].Seq != 1 || evs[2].Seq != 3 {
		t.Fatalf("events = %+v err=%v", evs, err)
	}
}

// Checkpoint write -> Latest -> Restore materializes the snapshotted workspace.
func TestCheckpointWriteLatestRestore(t *testing.T) {
	svc := NewService(newStore(t), NewLocalSnapshotter(t.TempDir()))
	ctx := context.Background()
	dir := t.TempDir()
	writeFile(t, dir, "work.txt", "v1")
	svc.AppendEvent(ctx, "j1", "start", nil)

	cp, err := svc.Write(ctx, "j1", dir, map[string]interface{}{"phase": "one"})
	if err != nil {
		t.Fatalf("write checkpoint: %v", err)
	}
	if cp.EventSeq != 1 {
		t.Errorf("checkpoint anchored at seq %d, want 1", cp.EventSeq)
	}

	latest, err := svc.Latest(ctx, "j1")
	if err != nil || latest.ID != cp.ID {
		t.Fatalf("latest = %+v err=%v", latest, err)
	}

	os.RemoveAll(dir)
	if _, err := svc.Restore(ctx, "j1", dir); err != nil {
		t.Fatalf("restore: %v", err)
	}
	got, _ := os.ReadFile(filepath.Join(dir, "work.txt"))
	if string(got) != "v1" {
		t.Errorf("restored work.txt = %q, want v1", got)
	}
}

// The ledger fires an effect exactly once; a second Do with the same key
// returns the cached result without re-invoking fn.
func TestLedgerDoOnce(t *testing.T) {
	ledger := NewLedger(newStore(t))
	ctx := context.Background()
	calls := 0
	fn := func() (string, error) { calls++; return "result://x", nil }

	uri1, err := ledger.Do(ctx, "j1", 5, "push@sha", "git-push", fn)
	if err != nil || uri1 != "result://x" {
		t.Fatalf("first Do: uri=%q err=%v", uri1, err)
	}
	uri2, err := ledger.Do(ctx, "j1", 5, "push@sha", "git-push", fn)
	if err != nil || uri2 != "result://x" {
		t.Fatalf("second Do: uri=%q err=%v", uri2, err)
	}
	if calls != 1 {
		t.Errorf("fn called %d times, want 1 (replay must short-circuit)", calls)
	}
}

// #37 exit: kill a run mid-loop; on restart it restores the last checkpoint,
// replays the event tail, and finishes — with no external effect fired twice.
// Deterministic local work (workspace writes) IS replayed; ledger-guarded
// external effects are NOT.
func TestResumeNoDoubleFire(t *testing.T) {
	st := newStore(t)
	snap := NewLocalSnapshotter(t.TempDir())
	svc := NewService(st, snap)
	ledger := NewLedger(st)
	ctx := context.Background()
	dir := t.TempDir()
	const jobID = "job-1"

	steps := []string{"plan", "edit", "push", "comment"}
	external := map[string]bool{"push": true, "comment": true} // effects outside the workspace
	fired := map[string]int{}

	// runLoop executes steps [from, end). crashAfter>=0 aborts after that index.
	runLoop := func(from, crashAfter int) string {
		for i := from; i < len(steps); i++ {
			name, seq := steps[i], int64(i+1)
			if _, err := svc.AppendEvent(ctx, jobID, name, map[string]interface{}{"step": name}); err != nil {
				t.Fatal(err)
			}
			// Deterministic local work — always re-runs on replay.
			writeFile(t, dir, name+".txt", name)
			// External side effect — ledger-guarded, must fire once.
			if external[name] {
				if _, err := ledger.Do(ctx, jobID, seq, name, "external", func() (string, error) {
					fired[name]++
					return "ok://" + name, nil
				}); err != nil {
					t.Fatal(err)
				}
			}
			// Checkpoint every 2 steps, so the crash leaves a non-empty tail to replay.
			if seq%2 == 0 {
				if _, err := svc.Write(ctx, jobID, dir, map[string]interface{}{"last": name}); err != nil {
					t.Fatal(err)
				}
			}
			if i == crashAfter {
				return "CRASHED"
			}
		}
		return "SUCCEEDED"
	}

	// First attempt crashes after "push" (index 2) — after its effect fired but
	// before the next checkpoint.
	if s := runLoop(0, 2); s != "CRASHED" {
		t.Fatalf("first run = %s, want CRASHED", s)
	}

	// Restart: wipe the workspace and restore the last durable checkpoint.
	os.RemoveAll(dir)
	cp, err := svc.Restore(ctx, jobID, dir)
	if err != nil {
		t.Fatalf("restore: %v", err)
	}

	// Resume from the event after the checkpoint. This REPLAYS "push", whose
	// external effect is already committed — the ledger must not re-fire it.
	if s := runLoop(int(cp.EventSeq), -1); s != "SUCCEEDED" {
		t.Fatalf("resume = %s, want SUCCEEDED", s)
	}

	if fired["push"] != 1 {
		t.Errorf("push effect fired %d times, want 1 (no double-fire on replay)", fired["push"])
	}
	if fired["comment"] != 1 {
		t.Errorf("comment effect fired %d times, want 1", fired["comment"])
	}
	// Workspace fully rebuilt by deterministic replay.
	for _, name := range steps {
		if _, err := os.Stat(filepath.Join(dir, name+".txt")); err != nil {
			t.Errorf("missing %s.txt after resume: %v", name, err)
		}
	}
}
