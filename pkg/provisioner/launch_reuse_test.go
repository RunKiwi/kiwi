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
		// `docker image inspect` resolves the desired image id; `docker inspect`
		// answers for the container. Both report the same id here, so the running
		// container is current and reuse is the correct outcome.
		"if [ \"$1\" = \"image\" ]; then echo sha256:same; exit 0; fi\n" +
		"if [ \"$1\" = \"inspect\" ] && [ \"$2\" = \"-f\" ] && [ \"$3\" = \"{{.Image}}\" ]; then echo sha256:same; exit 0; fi\n" +
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
	handle, err := d.Launch(context.Background(), "org_abc", "fleet", "tok", "https://api.example", true)
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
	if _, err := d.Launch(context.Background(), "org_abc", "fleet", "tok", "https://api.example", true); err != nil {
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

// fakeDockerStale is fakeDocker with the running container reporting a
// DIFFERENT image id than the one the launcher wants — a container left over
// from before a deploy.
func fakeDockerStale(t *testing.T) (logPath string) {
	t.Helper()
	dir := t.TempDir()
	logPath = filepath.Join(dir, "calls.log")

	script := "#!/bin/sh\n" +
		"echo \"$@\" >> " + logPath + "\n" +
		// The image the launcher wants.
		"if [ \"$1\" = \"image\" ]; then echo sha256:new; exit 0; fi\n" +
		// The image the running container was started from.
		"if [ \"$1\" = \"inspect\" ] && [ \"$3\" = \"{{.Image}}\" ]; then echo sha256:old; exit 0; fi\n" +
		// It is running.
		"if [ \"$1\" = \"inspect\" ]; then echo true; exit 0; fi\n" +
		"exit 0\n"

	if err := os.WriteFile(filepath.Join(dir, "docker"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("KIWI_HOST_CACHE_ROOT", filepath.Join(dir, "cache"))
	return logPath
}

// A container left on a previous image must be retired once the org is idle.
//
// Reusing a running container unconditionally removed what had been an
// accidental refresh mechanism: the old `rm -f` recycled the container on every
// submit, so an image roll landed as a side effect. Without that, a long-lived
// daemon served the previous build indefinitely — a deploy reporting success
// and changing nothing. Observed for real: the fleet ran a superseded daemon
// while `kiwidaemon:latest` already pointed at the fix.
func TestLaunchReplacesAStaleImageWhenOrgIsIdle(t *testing.T) {
	logPath := fakeDockerStale(t)

	d := NewDockerLauncher()
	if _, err := d.Launch(context.Background(), "org_abc", "fleet", "tok", "https://api.example", true); err != nil {
		t.Fatalf("Launch: %v", err)
	}

	calls := readCalls(t, logPath)
	if !strings.Contains(calls, "rm -f") || !strings.Contains(calls, "run -d") {
		t.Errorf("a stale container on an idle org must be replaced, or deploys never reach the fleet:\n%s", calls)
	}
}

// ...but not while that daemon is mid-task. Killing a busy one strands its
// lease for the full TTL, which is the twelve-minutes-for-two-minutes-of-work
// bug. A stale-but-working daemon is kept and retired on a later launch.
func TestLaunchKeepsAStaleImageWhileTheOrgIsBusy(t *testing.T) {
	logPath := fakeDockerStale(t)

	d := NewDockerLauncher()
	if _, err := d.Launch(context.Background(), "org_abc", "fleet", "tok", "https://api.example", false); err != nil {
		t.Fatalf("Launch: %v", err)
	}

	calls := readCalls(t, logPath)
	if strings.Contains(calls, "rm -f") {
		t.Errorf("a BUSY daemon was killed to pick up a new image — this strands the lease:\n%s", calls)
	}
}
