package daemon

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// How much memory a sandbox may use.
//
// This was the constant "512m", for every repository and every language. A
// `go test ./...` over a large module compiles well past that, and the way it
// fails is the problem: under gVisor the process the kernel picks is the
// sentry, so the sandbox does not report a failing test — it stops existing.
// Every command in that session afterwards returns "No such container", which
// is not a test result, cannot be acted on as one, and is what the model is
// handed anyway.
//
// Confirmed in production on 2026-08-12: a memcg OOM killed a sandbox 72
// seconds into a baseline, on a host with 11 GB free. The limit was never a
// property of the machine; it was a number.
const (
	// sandboxMemoryShare is the fraction of the host one sandbox may take. The
	// free fleet runs several orgs' daemons at once, so a limit generous enough
	// to evict its neighbours would move this failure rather than fix it.
	sandboxMemoryShare = 8
	// A Go or Node build needs about a gigabyte. Below this the limit is not
	// worth having, because everything hits it.
	sandboxMemoryFloor = 1 << 30
	// One test command does not need more than this, however large the host.
	sandboxMemoryCeiling = 4 << 30
	// Used when the host's memory cannot be read. Deliberately not zero: docker
	// reads zero as unlimited, which is the opposite of a safe fallback.
	sandboxMemoryUnknownHost = 2 << 30
)

// sandboxMemoryLimit is the docker --memory value for a sandbox, honouring the
// operator override.
//
// The override is what makes a genuinely memory-hungry repository runnable
// without a code change — the case this whole file exists to stop being a wall.
func sandboxMemoryLimit() string {
	if v := strings.TrimSpace(os.Getenv("KIWI_SANDBOX_MEMORY")); v != "" {
		return v
	}
	return sandboxMemoryFor(hostMemoryBytes())
}

// sandboxMemoryFor sizes one sandbox against a host of the given size. A host
// of 0 means "unknown".
func sandboxMemoryFor(hostBytes int64) string {
	var share int64 = sandboxMemoryUnknownHost
	if hostBytes > 0 {
		share = hostBytes / sandboxMemoryShare
	}
	if share < sandboxMemoryFloor {
		share = sandboxMemoryFloor
	}
	if share > sandboxMemoryCeiling {
		share = sandboxMemoryCeiling
	}
	// Whole megabytes: docker accepts a suffix, not a fraction.
	return fmt.Sprintf("%dm", share/(1<<20))
}

// hostMemoryBytes reports the machine's total memory, or 0 if it cannot be read.
func hostMemoryBytes() int64 { return hostMemoryBytesFrom("/proc/meminfo") }

// hostMemoryBytesFrom reads MemTotal out of a meminfo file.
//
// The daemon is itself a container, but the sandbox it launches is a SIBLING on
// the same host — so the host's capacity is the right number to divide, not the
// daemon's own cgroup limit.
func hostMemoryBytesFrom(path string) int64 {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(b), "\n") {
		if !strings.HasPrefix(line, "MemTotal:") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			return 0
		}
		kb, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil {
			return 0
		}
		return kb * 1024
	}
	return 0
}
