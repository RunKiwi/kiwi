package daemon

import (
	"strings"
	"testing"
)

// The exact output that reached the model, twice, on job_e3a491f48809d606.
// Verification had been impossible since the sandbox was OOM-killed six minutes
// earlier; the session went on for two more rounds and $3 before a stall rail
// stopped it and blamed the agent for "not making progress".
const deadBoxOutput = "Error response from daemon: No such container: kiwi-sess-108d1d21a395fd9a"

// The gVisor sentry's corpse. The kernel kills the sentry rather than the
// process inside it, so this — not an OOM message — is what a sandbox that ran
// out of memory actually prints.
const sentryDiedOutput = `?   	github.com/flexprice/flexprice	[no test files]
FAIL	github.com/flexprice/flexprice/api/custom/go [build failed]
waiting on pid 2: waiting on PID 2 in sandbox "1394981878ab459a477074ee84405268e1980ce807ee86d266b3ade7e71bbdfc": urpc method "containerManager.WaitPID" failed: EOF`

func TestSandboxLostRecognisesADeadContainer(t *testing.T) {
	why := sandboxLost(deadBoxOutput)
	if why == "" {
		t.Fatal("a removed container was read as a test result")
	}
	if !strings.Contains(strings.ToLower(why), "sandbox") {
		t.Errorf("reason %q should say the sandbox is the problem, not the code", why)
	}
}

func TestSandboxLostRecognisesTheSentryBeingKilled(t *testing.T) {
	why := sandboxLost(sentryDiedOutput)
	if why == "" {
		t.Fatal("a dead gVisor sentry was read as a test result")
	}
	// This is the one worth naming precisely: the operator's fix is more
	// memory, and nothing in the raw output says so.
	if !strings.Contains(strings.ToLower(why), "memory") {
		t.Errorf("reason %q should point at memory, which is the actual fix", why)
	}
}

func TestSandboxLostRecognisesAStoppedContainer(t *testing.T) {
	if sandboxLost("Error response from daemon: Container abc is not running") == "" {
		t.Error("a stopped container was read as a test result")
	}
	if sandboxLost("cannot exec in a stopped container") == "" {
		t.Error("exec into a stopped container was read as a test result")
	}
}

// The expensive mistake would be the other direction: classifying a real
// failing test as an infrastructure fault, which would fail tasks that are
// working correctly.
func TestSandboxLostIgnoresOrdinaryFailures(t *testing.T) {
	ordinary := []string{
		"--- FAIL: TestDivide (0.00s)\n    math_test.go:12: got 0, want 2",
		"api/custom/go/async.go:56:17: undefined: Flexprice",
		"FAIL\tgithub.com/acme/api [build failed]",
		"sh: npm: not found",
		"panic: runtime error: index out of range [3] with length 2",
		"Error: connect ECONNREFUSED 127.0.0.1:5432",
		"",
		// Names a container without saying anything is wrong with it.
		"ok  \tgithub.com/acme/container\t0.4s",
	}
	for _, out := range ordinary {
		if why := sandboxLost(out); why != "" {
			t.Errorf("sandboxLost(%q) = %q; this is a real test result and must reach the model", out, why)
		}
	}
}
