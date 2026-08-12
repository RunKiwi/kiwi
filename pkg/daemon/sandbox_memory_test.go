package daemon

import (
	"os"
	"path/filepath"
	"testing"
)

// 512 MB was the hardcoded limit, and it is the reason this file exists.
//
// A `go test ./...` over a large module compiles far past it, and under gVisor
// the process the kernel kills is the sentry — so the sandbox does not report a
// failing test, it ceases to exist. Every later command in that session then
// returns "No such container", which is not a test result and cannot be acted
// on by anything reading it as one. Confirmed in production: a memcg OOM at
// 4.8 GB total-vm killed a sandbox 72 seconds into a baseline on a host with
// 16 GB free.
func TestSandboxMemoryIsWellAboveTheLimitThatOOMed(t *testing.T) {
	got := sandboxMemoryFor(16 << 30) // a 16 GB host, which is what the fleet runs
	if got == "512m" {
		t.Fatal("still 512m: the limit that OOM-killed gVisor on a 16 GB host")
	}
	if bytes := parseMemBytes(t, got); bytes < 1<<30 {
		t.Errorf("sandboxMemoryFor(16 GB) = %q; a Go build needs at least 1 GB", got)
	}
}

// The host is shared. A limit that lets one sandbox take the whole machine
// would turn a single large build into an outage for every other org on the
// fleet — the failure this is meant to prevent, moved rather than fixed.
func TestSandboxMemoryLeavesRoomForOtherTasks(t *testing.T) {
	const host = 16 << 30
	got := parseMemBytes(t, sandboxMemoryFor(host))
	if got > host/4 {
		t.Errorf("sandboxMemoryFor(16 GB) = %d bytes, more than a quarter of the host; "+
			"concurrent tasks on a shared fleet would evict each other", got)
	}
}

// A small host must still get something a build can run in, and a very large
// one must not hand over an unbounded amount to a single test command.
func TestSandboxMemoryIsBounded(t *testing.T) {
	small := parseMemBytes(t, sandboxMemoryFor(1<<30)) // 1 GB host
	if small < 512<<20 {
		t.Errorf("a 1 GB host got %d bytes; the floor should keep it usable", small)
	}
	huge := parseMemBytes(t, sandboxMemoryFor(512<<30)) // 512 GB host
	if huge > 8<<30 {
		t.Errorf("a 512 GB host got %d bytes; one test command does not need that", huge)
	}
}

// Zero means "could not read the host", which must not produce a zero limit —
// docker reads that as unlimited, which is the opposite of a safe fallback.
func TestSandboxMemoryUnknownHostFallsBackToAUsableDefault(t *testing.T) {
	got := sandboxMemoryFor(0)
	if got == "" || got == "0" {
		t.Fatalf("sandboxMemoryFor(0) = %q; an unknown host must still be capped", got)
	}
	if parseMemBytes(t, got) < 1<<30 {
		t.Errorf("sandboxMemoryFor(0) = %q; the fallback should still fit a Go build", got)
	}
}

// The operator override is what makes a genuinely memory-hungry repository
// runnable without a code change.
func TestSandboxMemoryHonoursTheOperatorOverride(t *testing.T) {
	t.Setenv("KIWI_SANDBOX_MEMORY", "6g")
	if got := sandboxMemoryLimit(); got != "6g" {
		t.Errorf("sandboxMemoryLimit() = %q, want the override", got)
	}
}

func TestHostMemoryBytesReadsMeminfo(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "meminfo")
	if err := os.WriteFile(path, []byte("MemTotal:       16389012 kB\nMemFree: 100 kB\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := hostMemoryBytesFrom(path)
	if want := int64(16389012) * 1024; got != want {
		t.Errorf("hostMemoryBytesFrom = %d, want %d", got, want)
	}
	// An unreadable file is reported as unknown rather than as zero memory.
	if got := hostMemoryBytesFrom(filepath.Join(dir, "nope")); got != 0 {
		t.Errorf("missing meminfo = %d, want 0 (unknown)", got)
	}
}

func parseMemBytes(t *testing.T, s string) int64 {
	t.Helper()
	if s == "" {
		t.Fatal("empty memory limit")
	}
	unit := s[len(s)-1]
	var mult int64
	switch unit {
	case 'g':
		mult = 1 << 30
	case 'm':
		mult = 1 << 20
	default:
		t.Fatalf("unrecognised memory limit %q; docker wants a g/m suffix", s)
	}
	var n int64
	for _, c := range s[:len(s)-1] {
		if c < '0' || c > '9' {
			t.Fatalf("unrecognised memory limit %q", s)
		}
		n = n*10 + int64(c-'0')
	}
	return n * mult
}
