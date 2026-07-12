package orchestrator

import (
	"testing"

	"github.com/ibreakthecloud/kiwi/pkg/store"
)

func TestRecoverAction(t *testing.T) {
	dir := t.TempDir()
	if got := recoverAction(&dir); got != "relaunch" {
		t.Errorf("existing dir: got %q want relaunch", got)
	}
	missing := dir + "/missing"
	if got := recoverAction(&missing); got != "fail" {
		t.Errorf("missing dir: got %q want fail", got)
	}
	empty := ""
	if got := recoverAction(&empty); got != "fail" {
		t.Errorf("empty path: got %q want fail", got)
	}
	if got := recoverAction(nil); got != "fail" {
		t.Errorf("nil path: got %q want fail", got)
	}
}

func TestRecoverTasks(t *testing.T) {
	db := newTestDB(t)
	liveDir := t.TempDir() // exists → relaunch
	goneDir := "/no/such/dir-xyz"

	db.Create(&store.Job{ID: "live", Status: "RUNNING", SandboxRef: &liveDir, Inputs: map[string]interface{}{"task": "t", "file": "a.go", "test_cmd": "go test"}})
	db.Create(&TaskState{ID: "live", Status: "RUNNING"})

	db.Create(&store.Job{ID: "gone", Status: "PAUSED", SandboxRef: &goneDir})
	db.Create(&TaskState{ID: "gone", Status: "PAUSED"})

	db.Create(&store.Job{ID: "done", Status: "SUCCESS", SandboxRef: &liveDir})
	db.Create(&TaskState{ID: "done", Status: "SUCCESS"})

	var launched []string
	s := &Server{db: db}
	s.launchFn = func(taskID, sandboxPath, task, file, testCmd string) {
		launched = append(launched, taskID)
	}

	s.RecoverTasks()

	if len(launched) != 1 || launched[0] != "live" {
		t.Fatalf("expected only 'live' relaunched, got %v", launched)
	}
	var gone store.Job
	db.First(&gone, "id = ?", "gone")
	if gone.Status != "FAILED" {
		t.Errorf("gone status: got %q want FAILED", gone.Status)
	}
	var done store.Job
	db.First(&done, "id = ?", "done")
	if done.Status != "SUCCESS" {
		t.Errorf("done status changed: %q", done.Status)
	}
}
