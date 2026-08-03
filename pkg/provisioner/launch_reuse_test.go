package provisioner

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeDocker puts a stub `docker` on PATH that appends each invocation's
// arguments to a log file, and makes `docker inspect` report `running`.
//
// Shelling out is the thing under test here — whether Launch issues `rm -f`
// against a live container — so the seam has to be the command itself rather
// than a Go interface.
func fakeDocker(t *testing.T, running bool) (logPath string) {
	t.Helper()
	dir := t.TempDir()
	logPath = filepath.Join(dir, "calls.log")

	inspect := "false"
	exitForInspect := "0"
	if running {
		inspect = "true"
	} else {
		// A container that does not exist makes `docker inspect` exit non-zero,
		// which is exactly how a first-ever launch looks.
		exitForInspect = "1"
	}

	script := "#!/bin/sh\n" +
		"echo \"$@\" >> " + logPath + "\n" +
		"if [ \"$1\" = \"inspect\" ]; then echo " + inspect + "; exit " + exitForInspect + "; fi\n" +
		"exit 0\n"

	if err := os.WriteFile(filepath.Join(dir, "docker"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	// Keep the bind-mount root out of /var/lib/kiwi.
	t.Setenv("KIWI_HOST_CACHE_ROOT", filepath.Join(dir, "cache"))
	return logPath
}

func readCalls(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(b)
}

// Launching for an org whose daemon is already running must not touch it.
//
// The container is named per org, not per task, and Launch used to open with an
// unconditional `docker rm -f`. So a second task submitted while the first was
// still running killed the daemon executing it. The task stayed LEASED with
// nothing running it, and since nothing detects a vanished daemon it waited out
// the full 10-minute lease before a retry picked it up — a two-minute task
// taking twelve, with ten of them spent on a lease owned by a dead process.
func TestLaunchDoesNotKillARunningDaemon(t *testing.T) {
	logPath := fakeDocker(t, true)

	d := NewDockerLauncher()
	handle, err := d.Launch(context.Background(), "org_abc", "fleet", "tok", "https://api.example")
	if err != nil {
		t.Fatalf("Launch: %v", err)
	}
	if handle != Handle("kiwi-free-org-org_abc") {
		t.Errorf("handle = %q", handle)
	}

	calls := readCalls(t, logPath)
	if strings.Contains(calls, "rm -f") {
		t.Errorf("Launch force-removed a RUNNING container — this is the bug that orphaned leases:\n%s", calls)
	}
	if strings.Contains(calls, "run -d") {
		t.Errorf("Launch started a second container for an org that already has one:\n%s", calls)
	}
}

// The case the original `rm -f` was written for: a stopped or half-created
// container still owns the name, so it must be removed before a fresh launch.
func TestLaunchReplacesAContainerThatIsNotRunning(t *testing.T) {
	logPath := fakeDocker(t, false)

	d := NewDockerLauncher()
	if _, err := d.Launch(context.Background(), "org_abc", "fleet", "tok", "https://api.example"); err != nil {
		t.Fatalf("Launch: %v", err)
	}

	calls := readCalls(t, logPath)
	if !strings.Contains(calls, "rm -f") {
		t.Errorf("a dead container still owns the name and must be removed:\n%s", calls)
	}
	if !strings.Contains(calls, "run -d") {
		t.Errorf("Launch should have started a container:\n%s", calls)
	}
}
