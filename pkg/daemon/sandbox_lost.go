package daemon

import "strings"

// The sandbox stopped existing.
//
// This is a third category, alongside "the test failed" and "the sandbox is
// wrong" (envfault.go). Both of those describe a container that ran the command
// and printed something about it. Here nothing ran at all: the container is
// gone, and docker says so on stderr, which the daemon then hands to the model
// as though it were the repository's opinion of its own code.
//
// Seen in production on job_e3a491f48809d606. A session's box was OOM-killed
// during the baseline; every verification for the next six minutes returned
// "No such container", and the Implementer — with no working signal — explored
// for two rounds and edited nothing. Both rounds therefore ended in an
// identical state, and the no-progress rail stopped the session with "the
// session is not making progress", which named the agent for an infrastructure
// fault it could neither see nor fix.
//
// The distinction that matters: an envFault is repairable and worth retrying
// with a different image. A lost sandbox is not a fact about the repository at
// all, and continuing to spend a customer's budget on rounds that cannot be
// verified is the failure this prevents.

// sandboxLost reports why the container that should have run the command is
// gone, or "" when the output is something a model can act on.
//
// Matching is on docker's and gVisor's own wording. The bar for adding a
// pattern is that it must be impossible for a test suite to print it as part of
// an ordinary failure — a false positive fails a task that was working.
func sandboxLost(output string) string {
	lower := strings.ToLower(output)

	// gVisor's sentry died. Checked first: when the kernel OOM-kills a sandbox
	// it kills the sentry, not the process inside it, so this is what a test
	// command that ran out of memory actually prints. Nothing in it says
	// "memory", which is why the raw output is useless to whoever has to fix it.
	if strings.Contains(lower, "containermanager.waitpid") ||
		(strings.Contains(lower, "urpc method") && strings.Contains(lower, "failed: eof")) {
		return "the sandbox died while the command was running, which is what an out-of-memory kill looks like under gVisor. " +
			"Raise the sandbox memory limit (KIWI_SANDBOX_MEMORY) if this repository's build needs more than the default."
	}

	// docker: the container was removed, or is no longer running.
	if strings.Contains(lower, "no such container") ||
		strings.Contains(lower, "cannot exec in a stopped container") ||
		(strings.Contains(lower, "response from daemon") && strings.Contains(lower, "is not running")) {
		return "the sandbox container this task was using no longer exists, so the test command could not be run at all. " +
			"The container was most likely killed for exceeding its memory limit."
	}

	return ""
}
