package orchestrator

import (
	"testing"
)

func TestRecoverAction(t *testing.T) {
	dir := t.TempDir()
	if got := recoverAction(dir); got != "relaunch" {
		t.Errorf("existing dir: got %q want relaunch", got)
	}
	if got := recoverAction(dir + "/missing"); got != "fail" {
		t.Errorf("missing dir: got %q want fail", got)
	}
	if got := recoverAction(""); got != "fail" {
		t.Errorf("empty path: got %q want fail", got)
	}
}

func TestRecoverTasks(t *testing.T) {
	db := newTestDB(t)
	liveDir := t.TempDir() // exists → relaunch
	db.Create(&TaskState{ID: "live", Status: "RUNNING", SandboxPath: liveDir, Task: "t", FilePath: "a.go", TestCmd: "go test"})
	db.Create(&TaskState{ID: "gone", Status: "PAUSED", SandboxPath: "/no/such/dir-xyz"})
	db.Create(&TaskState{ID: "done", Status: "SUCCESS", SandboxPath: liveDir})

	var launched []string
	s := &Server{db: db}
	s.launchFn = func(taskID, sandboxPath, task, file, testCmd string) {
		launched = append(launched, taskID)
	}

	s.RecoverTasks()

	if len(launched) != 1 || launched[0] != "live" {
		t.Fatalf("expected only 'live' relaunched, got %v", launched)
	}
	var gone TaskState
	db.First(&gone, "id = ?", "gone")
	if gone.Status != "FAILED" {
		t.Errorf("gone status: got %q want FAILED", gone.Status)
	}
	var done TaskState
	db.First(&done, "id = ?", "done")
	if done.Status != "SUCCESS" {
		t.Errorf("done status changed: %q", done.Status)
	}
}
