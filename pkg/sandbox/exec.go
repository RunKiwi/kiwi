package sandbox

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
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
	Runtime     string // e.g. "runc", "runsc"
	NetworkNone bool   // e.g. --network=none
	// Mounts are extra bind mounts as "host:container" specs. They carry the
	// package caches that must outlive a single container: a language toolchain
	// downloads into its own home (/go/pkg/mod, ~/.cargo), not into the project
	// directory, so without these the install phase's work is destroyed when its
	// container exits and verification finds nothing.
	//
	// Host paths, so they must mean the same thing to whichever docker daemon
	// runs the container — see the provisioner's launch mount.
	Mounts []string
}

// Driver defines the interface for running a command in an isolated environment.
type Driver interface {
	Run(ctx context.Context, dir string, cmdStr string, env []string, cfg *SandboxConfig) (*Result, error)
}

// RunCommand runs a test or compiler command in a shell inside the target directory.
// It retrieves tenant-specific resource constraints from the context if present.
func RunCommand(ctx context.Context, dir string, cmdStr string, env []string) (*Result, error) {
	var cfg *SandboxConfig
	if c, ok := ctx.Value(SandboxConfigKey).(*SandboxConfig); ok && c != nil {
		cfg = c
	} else {
		cfg = &SandboxConfig{
			UseDocker: os.Getenv("USE_DOCKER") == "true",
		}
	}

	sandboxEnv := os.Getenv("KIWI_SANDBOX")
	if sandboxEnv == "firecracker" {
		return runFirecracker(ctx, dir, cmdStr, env, cfg)
	}

	if cfg.UseDocker {
		return runDocker(ctx, dir, cmdStr, env, cfg)
	}

	return runLocal(ctx, dir, cmdStr, env)
}

func runLocal(ctx context.Context, dir string, cmdStr string, env []string) (*Result, error) {
	cmd := exec.CommandContext(ctx, "sh", "-c", cmdStr)
	cmd.Dir = dir
	if len(env) > 0 {
		cmd.Env = append(os.Environ(), env...)
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

func runDocker(ctx context.Context, dir string, cmdStr string, env []string, cfg *SandboxConfig) (*Result, error) {
	dockerImage := "golang:1.25-alpine"
	if cfg.DockerImage != "" {
		dockerImage = cfg.DockerImage
	}

	args, envFile, err := buildDockerArgs(dir, cmdStr, env, cfg, dockerImage)
	if err != nil {
		return nil, err
	}
	if envFile != "" {
		defer os.Remove(envFile)
	}

	cmd := exec.CommandContext(ctx, "docker", args...)
	// The values behind the `-e NAME` flags above. They travel in the docker
	// CLI's environment rather than its arguments, so they never appear in the
	// process table.
	if len(env) > 0 {
		cmd.Env = append(os.Environ(), env...)
	}

	var outBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &outBuf

	err = cmd.Run()
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

// buildDockerArgs constructs the docker run arguments and optionally an env file path.
func buildDockerArgs(dir string, cmdStr string, env []string, cfg *SandboxConfig, dockerImage string) ([]string, string, error) {
	args := []string{"run", "--rm", "-i"}
	args = append(args, "-v", fmt.Sprintf("%s:/workspace", dir), "-w", "/workspace")

	for _, m := range cfg.Mounts {
		args = append(args, "-v", m)
	}

	if cfg.MemoryLimit != "" {
		args = append(args, "--memory", cfg.MemoryLimit)
	}
	if cfg.CPULimit != "" {
		args = append(args, "--cpus", cfg.CPULimit)
	}
	if cfg.NetworkNone {
		args = append(args, "--network", "none")
	}
	if cfg.Runtime != "" {
		args = append(args, "--runtime", cfg.Runtime)
	}

	// Environment is passed as `-e NAME`, with the value supplied through the
	// docker CLI's own environment (see RunCommand). Two properties matter, and
	// only this form has both.
	//
	// It survives newlines. The previous form wrote a temp --env-file, one
	// KEY=VALUE per line, which cannot represent a multi-line value: TASK holds
	// the planner's description, and the moment that description had a second
	// line docker rejected the whole file —
	//
	//	docker: --env-file: invalid env file: variable '1. Inspect the repo' contains whitespaces
	//
	// — so the container never started, every command "failed" identically, and
	// the loop stalled. No test command could pass, in any repository.
	//
	// And it keeps secrets out of the process table, which is why the env file
	// was chosen originally: `-e NAME` puts only the NAME in argv, so a
	// credential is not visible to `ps` on a host shared by other tenants.
	for _, eVal := range env {
		name, _, ok := strings.Cut(eVal, "=")
		if !ok || name == "" {
			continue
		}
		args = append(args, "-e", name)
	}

	args = append(args, dockerImage, "sh", "-c", cmdStr)
	return args, "", nil
}
