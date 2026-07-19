package daemon

import (
	"os"
	"path/filepath"
	"strings"
)

// maxAgentMDBytes caps how much of a repo's AGENT.md is injected into a worker
// prompt, so an oversized (or hostile) file can't blow up the context.
const maxAgentMDBytes = 32 * 1024

// repoContext returns the repository's AGENT.md contents (if present at the
// worktree root) as per-repo context to prepend to a worker's prompt — repo
// conventions, how to run tests, what not to touch. It is NOT a persona and is
// not generated per worker (Execution Model RFC §5). Absence is a clean no-op.
func repoContext(worktreePath string) string {
	data, err := os.ReadFile(filepath.Join(worktreePath, "AGENT.md"))
	if err != nil {
		return "" // absent or unreadable → no context, no error
	}
	if len(data) > maxAgentMDBytes {
		data = data[:maxAgentMDBytes]
	}
	return strings.TrimSpace(string(data))
}

// withRepoContext prepends repo context to a task description under a clear
// header so the Actor can distinguish project conventions from the task itself.
func withRepoContext(description, context string) string {
	if context == "" {
		return description
	}
	return "# Repository context (AGENT.md)\n" + context + "\n\n# Task\n" + description
}
