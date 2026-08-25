package sandbox

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Result holds the execution outcome of a command run in the sandbox
type Result struct {
	Success bool
	Output  string
	GitDiff string
	// ProvisionMs is how long the container took to reach State.Running,
	// measured independently of how long the command inside it then took —
	// see watchProvisioning. Zero means unmeasured (local/firecracker mode,
	// or a command fast enough that the container was gone before the first
	// poll could confirm it started), never a claim that startup was
	// instant.
	ProvisionMs int64
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

	name, err := containerName()
	if err != nil {
		return nil, err
	}

	args, envFile, err := buildDockerArgs(dir, cmdStr, env, cfg, dockerImage, name)
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

	// This container is named (rather than anonymous) so its own started-at
	// timestamp can be read from a second, concurrent `docker inspect` while
	// `cmd.Run()` below is still blocked on the command inside it — the point
	// being that provisioning time must not depend on how long, or whether,
	// the command itself finishes. See watchProvisioning.
	callStart := time.Now()
	provisionCh := make(chan int64, 1)
	go func() { provisionCh <- watchProvisioning(ctx, name, callStart) }()

	err = cmd.Run()
	output := outBuf.String()

	// The command has already returned by this point, so the container (kept
	// with --rm) may already be gone — give the watcher a short grace period
	// to have confirmed a start it saw before that happened, rather than
	// blocking on its full deadline. In the overwhelming common case the
	// watcher's first poll already landed well before cmd.Run() returned, so
	// this reads the buffered channel instantly.
	var provisionMs int64
	select {
	case provisionMs = <-provisionCh:
	case <-time.After(300 * time.Millisecond):
	}

	// Capture the current git diff to observe changes
	diffCmd := exec.CommandContext(ctx, "git", "diff")
	diffCmd.Dir = dir
	var diffBuf bytes.Buffer
	diffCmd.Stdout = &diffBuf
	_ = diffCmd.Run() // Ignore errors if directory is not a git repo

	return &Result{
		Success:     err == nil,
		Output:      output,
		GitDiff:     diffBuf.String(),
		ProvisionMs: provisionMs,
	}, nil
}

// containerName returns a unique per-invocation container name so its
// lifecycle can be inspected independently of the anonymous default docker
// would otherwise assign.
func containerName() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("sandbox container name: %w", err)
	}
	return "kiwi-sbx-" + hex.EncodeToString(b), nil
}

// watchProvisioning polls for a container's State.StartedAt, independent of
// whether the command running inside it ever finishes or succeeds — so a
// task that later times out or gets killed still reports how long its own
// sandbox took to become ready, instead of that number only ever being
// knowable after the fact.
//
// Docker reports the zero time ("0001-01-01T00:00:00Z") until the container
// actually starts, which is the sentinel checked below rather than treating
// any parseable timestamp as good enough.
//
// Returns 0 when the container could never be confirmed running before
// deadline — normally because a command fast enough to finish (and, with
// --rm, be removed) before the first poll landed, which is also the case
// where "cold start" was never a meaningful number for that call.
func watchProvisioning(ctx context.Context, name string, callStart time.Time) int64 {
	const pollInterval = 75 * time.Millisecond
	const maxWait = 3 * time.Second

	deadline := time.Now().Add(maxWait)
	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return 0
		case <-time.After(pollInterval):
		}

		out, err := exec.CommandContext(ctx, "docker", "inspect", "--format", "{{.State.StartedAt}}", name).Output()
		if err != nil {
			continue // not created yet, or already gone
		}
		startedAt, ok := parseContainerStartedAt(string(out))
		if !ok {
			continue // exists but hasn't started yet
		}
		if ms := startedAt.Sub(callStart).Milliseconds(); ms > 0 {
			return ms
		}
		return 0
	}
	return 0
}

// parseContainerStartedAt parses `docker inspect --format '{{.State.StartedAt}}'`
// output, reporting false for a container that exists but has not started yet.
//
// Docker reports the Go zero time ("0001-01-01T00:00:00Z") in that case, which
// parses successfully as a timestamp — Year() <= 1 is what actually separates
// "not started" from a real instant, not the parse error alone.
func parseContainerStartedAt(raw string) (time.Time, bool) {
	t, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(raw))
	if err != nil || t.Year() <= 1 {
		return time.Time{}, false
	}
	return t, true
}

// sandboxLabel is the fixed label applied to all sandbox containers, so telemetry
// queries (docker stats, docker ps) can filter to managed containers and exclude
// unrelated workloads on the host.
const sandboxLabel = "io.kiwi.sandbox=true"

// buildDockerArgs constructs the docker run arguments and optionally an env file path.
func buildDockerArgs(dir string, cmdStr string, env []string, cfg *SandboxConfig, dockerImage string, name string) ([]string, string, error) {
	args := []string{"run", "--rm", "-i", "--name", name, "--label", sandboxLabel}
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
