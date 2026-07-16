package sandbox

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
)

// Result holds the execution outcome of a command run in the sandbox
type Result struct {
	Success bool
	Output  string
	GitDiff string
}

type contextKey int

const SandboxConfigKey contextKey = 0

// SandboxConfig holds tenant-specific container sandbox limits.
type SandboxConfig struct {
	UseDocker   bool
	DockerImage string
	MemoryLimit string // e.g. "512m"
	CPULimit    string // e.g. "1.0"
	NetworkNone bool   // e.g. --network=none
}

// RunCommand runs a test or compiler command in a shell inside the target directory.
// If the environment variable USE_DOCKER=true is set, it isolates execution inside a Docker container.
// It retrieves tenant-specific resource constraints from the context if present.
func RunCommand(ctx context.Context, dir string, cmdStr string, env []string) (*Result, error) {
	var cmd *exec.Cmd

	var useDocker bool
	var dockerImage = "golang:1.21-alpine"
	var memoryLimit string
	var cpuLimit string
	var networkNone bool

	if cfg, ok := ctx.Value(SandboxConfigKey).(*SandboxConfig); ok && cfg != nil {
		useDocker = cfg.UseDocker
		if cfg.DockerImage != "" {
			dockerImage = cfg.DockerImage
		}
		memoryLimit = cfg.MemoryLimit
		cpuLimit = cfg.CPULimit
		networkNone = cfg.NetworkNone
	} else {
		useDocker = os.Getenv("USE_DOCKER") == "true"
	}

	if useDocker {
		// Run isolated command inside Docker container
		args := []string{"run", "--rm", "-i"}
		// Mount target directory to container workspace
		args = append(args, "-v", fmt.Sprintf("%s:/workspace", dir), "-w", "/workspace")

		// Apply resource limits
		if memoryLimit != "" {
			args = append(args, "--memory", memoryLimit)
		}
		if cpuLimit != "" {
			args = append(args, "--cpus", cpuLimit)
		}
		if networkNone {
			args = append(args, "--network", "none")
		}

		// Inject environment variables using --env-file to avoid leaking secrets in ps output
		if len(env) > 0 {
			envFile, err := os.CreateTemp("", "kiwi-env-*.env")
			if err != nil {
				return nil, fmt.Errorf("failed to create temp env file: %w", err)
			}
			defer os.Remove(envFile.Name())

			for _, eVal := range env {
				envFile.WriteString(eVal + "\n")
			}
			envFile.Close()

			args = append(args, "--env-file", envFile.Name())
		}

		// Use configurable Docker image and execute command
		args = append(args, dockerImage, "sh", "-c", cmdStr)

		cmd = exec.CommandContext(ctx, "docker", args...)
	} else {
		// Local command execution
		cmd = exec.CommandContext(ctx, "sh", "-c", cmdStr)
		cmd.Dir = dir
		if len(env) > 0 {
			cmd.Env = append(os.Environ(), env...)
		}
	}

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
