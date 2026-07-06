package sandbox

import (
	"bytes"
	"context"
	"os/exec"
)

// Result holds the execution outcome of a command run in the sandbox
type Result struct {
	Success bool
	Output  string
	GitDiff string
}

// RunCommand runs a test or compiler command in a shell inside the target directory.
// It also fetches the current git diff of the workspace to capture modifications.
func RunCommand(ctx context.Context, dir string, cmdStr string) (*Result, error) {
	// Execute the command in sh/bash to allow wildcards, pipes, etc.
	cmd := exec.CommandContext(ctx, "sh", "-c", cmdStr)
	cmd.Dir = dir

	var outBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &outBuf

	err := cmd.Run()
	output := outBuf.String()

	// Capture the current git diff to observe changes
	diffCmd := exec.CommandContext(ctx, "git", "diff")
	diffCmd.Dir = dir
	var diffBuf bytes.Buffer
	diffCmd.Stdout = &diffBuf
	_ = diffCmd.Run() // Ignore errors if directory is not a git repo

	return &Result{
		Success: err == nil,
		Output:  output,
		GitDiff: diffBuf.String(),
	}, nil
}
