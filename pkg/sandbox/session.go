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

// A Session is a sandbox that stays up across many commands against one
// workspace.
//
// RunCommand starts a fresh `docker run --rm` per call, which is right when a
// task runs one command: the container is the isolation boundary and throwing
// it away between tasks is the point. An agentic round is different — it issues
// dozens of reads, greps and builds — and paying container startup for each,
// plus re-mounting the worktree and re-warming the toolchain's on-disk state,
// dominates the round. One container, many execs.
//
// What does not change is the isolation. A Session is created with the same
// image, runtime, resource caps and network policy RunCommand would have used,
// so it is the same box held open, not a weaker one. In particular it is
// offline whenever cfg.NetworkNone is set, and the networked dependency install
// stays where it is — a separate one-shot RunCommand with its own config and no
// credentials. Reaching for a networked exec inside this container would
// collapse the two-phase split it exists to create.
type Session struct {
	dir string
	cfg SandboxConfig
	// container is the running container's name, empty in local mode.
	container string
	closed    bool
}

// SessionOpts tunes a Session.
type SessionOpts struct {
	// TTL bounds how long the container may live. It is a backstop, not the
	// mechanism: Close is the normal path, and the caller's own deadline governs
	// the work. It exists because a daemon killed mid-round leaves the container
	// behind, and an orphan holding memory and a CPU share on a shared fleet
	// host is worse than a round that has to restart. Defaults to 2h.
	TTL time.Duration
}

const defaultSessionTTL = 2 * time.Hour

// NewSession starts a sandbox held open for repeated commands against dir.
//
// With cfg.UseDocker false it runs commands directly on the host, matching
// RunCommand's local mode — which is what tests and Docker-less development
// hosts use. Nothing is started in that case, and Close is a no-op.
func NewSession(ctx context.Context, dir string, cfg *SandboxConfig, opts SessionOpts) (*Session, error) {
	s := &Session{dir: dir}
	if cfg != nil {
		s.cfg = *cfg
	}
	if !s.cfg.UseDocker {
		return s, nil
	}

	ttl := opts.TTL
	if ttl <= 0 {
		ttl = defaultSessionTTL
	}

	name, err := sessionName()
	if err != nil {
		return nil, err
	}

	image := s.cfg.DockerImage
	if image == "" {
		image = "golang:1.25-alpine"
	}

	args := []string{"run", "-d", "--rm", "--name", name, "--label", sandboxLabel}
	args = append(args, "-v", fmt.Sprintf("%s:/workspace", dir), "-w", "/workspace")
	for _, m := range s.cfg.Mounts {
		args = append(args, "-v", m)
	}
	if s.cfg.MemoryLimit != "" {
		args = append(args, "--memory", s.cfg.MemoryLimit)
	}
	if s.cfg.CPULimit != "" {
		args = append(args, "--cpus", s.cfg.CPULimit)
	}
	if s.cfg.NetworkNone {
		args = append(args, "--network", "none")
	}
	if s.cfg.Runtime != "" {
		args = append(args, "--runtime", s.cfg.Runtime)
	}
	// A bounded sleep rather than `sleep infinity`: see SessionOpts.TTL. Busybox
	// and coreutils both accept a plain seconds argument.
	args = append(args, image, "sh", "-c", fmt.Sprintf("sleep %d", int(ttl.Seconds())))

	var out bytes.Buffer
	cmd := exec.CommandContext(ctx, "docker", args...)
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("start sandbox session: %v: %s", err, strings.TrimSpace(out.String()))
	}

	s.container = name
	return s, nil
}

// Image reports the image the session is running, which a caller may need in
// order to report or correct it.
func (s *Session) Image() string { return s.cfg.DockerImage }

// Local reports whether the session runs commands on the host rather than in a
// container.
func (s *Session) Local() bool { return s.container == "" }

// Exec runs one command in the session and returns its combined output.
//
// Unlike RunCommand the Result carries no GitDiff: a session's caller holds the
// worktree on the host and asks git directly, which is both cheaper and correct
// — the sandbox has no credentials and must not be the thing running git.
func (s *Session) Exec(ctx context.Context, cmdStr string, env []string) (*Result, error) {
	if s.closed {
		return nil, fmt.Errorf("sandbox session: already closed")
	}
	if s.container == "" {
		return s.execLocal(ctx, cmdStr, env)
	}

	args := []string{"exec"}
	// Values travel in the docker CLI's own environment, not in argv, so a
	// credential is never visible in the process table on a shared host. Same
	// reasoning as buildDockerArgs.
	for _, e := range env {
		name, _, ok := strings.Cut(e, "=")
		if !ok || name == "" {
			continue
		}
		args = append(args, "-e", name)
	}
	args = append(args, s.container, "sh", "-c", cmdStr)

	cmd := exec.CommandContext(ctx, "docker", args...)
	if len(env) > 0 {
		cmd.Env = append(os.Environ(), env...)
	}
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	return &Result{Success: err == nil, Output: out.String()}, nil
}

func (s *Session) execLocal(ctx context.Context, cmdStr string, env []string) (*Result, error) {
	cmd := exec.CommandContext(ctx, "sh", "-c", cmdStr)
	cmd.Dir = s.dir
	if len(env) > 0 {
		cmd.Env = append(os.Environ(), env...)
	}
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	return &Result{Success: err == nil, Output: out.String()}, nil
}

// Close tears the session down. It is safe to call more than once.
//
// It deliberately does not take the caller's context: a session is usually
// closed on the way out of a canceled or timed-out round, and using that
// context would skip the very cleanup the cancellation makes necessary.
func (s *Session) Close() error {
	if s.closed || s.container == "" {
		s.closed = true
		return nil
	}
	s.closed = true

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var out bytes.Buffer
	cmd := exec.CommandContext(ctx, "docker", "rm", "-f", s.container)
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("remove sandbox session %s: %v: %s", s.container, err, strings.TrimSpace(out.String()))
	}
	return nil
}

func sessionName() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("sandbox session name: %w", err)
	}
	return "kiwi-sess-" + hex.EncodeToString(b), nil
}
