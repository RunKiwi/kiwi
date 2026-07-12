package infra

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ibreakthecloud/kiwi/pkg/checkpoint"
	"github.com/ibreakthecloud/kiwi/pkg/sandbox"
	"github.com/ibreakthecloud/kiwi/pkg/store"
)

type DockerInfra struct {
	baseTempDir string
}

func NewDockerInfra(baseTempDir string) *DockerInfra {
	return &DockerInfra{baseTempDir: baseTempDir}
}

type DockerHandle struct {
	id          string
	sandboxPath string
	config      *sandbox.SandboxConfig
}

func (h *DockerHandle) ID() string {
	return h.id
}

func (h *DockerHandle) RunCommand(ctx context.Context, cmd string, env []string) (string, error) {
	ctx = context.WithValue(ctx, sandbox.SandboxConfigKey, h.config)
	res, err := sandbox.RunCommand(ctx, h.sandboxPath, cmd, env)
	if err != nil {
		return "", err
	}
	if !res.Success {
		return res.Output, ErrTestFailed
	}
	return res.Output, nil
}

func (h *DockerHandle) GetOutputArchive(ctx context.Context) ([]byte, error) {
	return sandbox.ZipDir(h.sandboxPath)
}

func (d *DockerInfra) Provision(ctx context.Context, sandboxPath string, manifest *store.Manifest) (Handle, error) {
	if sandboxPath == "" {
		return nil, fmt.Errorf("sandboxPath is required")
	}

	cfg, ok := ctx.Value(sandbox.SandboxConfigKey).(*sandbox.SandboxConfig)
	if !ok || cfg == nil {
		cfg = &sandbox.SandboxConfig{
			UseDocker:   os.Getenv("USE_DOCKER") == "true",
			DockerImage: "golang:1.21-alpine", // Default fallback
			MemoryLimit: "512m",
			CPULimit:    "1.0",
			NetworkNone: true,
		}
	} else {
		cfgCopy := *cfg
		cfg = &cfgCopy
	}

	// Apply manifest overrides if they exist
	if manifest != nil {
		if img, ok := manifest.Content["docker_image"].(string); ok && img != "" {
			cfg.DockerImage = img
		}
	}

	return &DockerHandle{
		id:          filepath.Base(sandboxPath),
		sandboxPath: sandboxPath,
		config:      cfg,
	}, nil
}

func (d *DockerInfra) Status(ctx context.Context, handle Handle) (string, error) {
	return "RUNNING", nil
}

func (d *DockerInfra) Snapshot(ctx context.Context, handle Handle) (*store.SnapshotRef, error) {
	dh, ok := handle.(*DockerHandle)
	if !ok {
		return nil, fmt.Errorf("invalid handle type")
	}
	uri, hash, err := checkpoint.NewLocalSnapshotter(d.baseTempDir).Snapshot(dh.sandboxPath)
	if err != nil {
		return nil, err
	}
	return &store.SnapshotRef{URI: uri, Hash: hash}, nil
}

func (d *DockerInfra) Restore(ctx context.Context, handle Handle, ref *store.SnapshotRef) error {
	dh, ok := handle.(*DockerHandle)
	if !ok {
		return fmt.Errorf("invalid handle type")
	}
	if ref == nil {
		return fmt.Errorf("invalid snapshot reference")
	}
	return checkpoint.NewLocalSnapshotter(d.baseTempDir).Restore(ref.URI, dh.sandboxPath)
}

func (d *DockerInfra) Terminate(ctx context.Context, handle Handle) error {
	dh, ok := handle.(*DockerHandle)
	if !ok {
		return fmt.Errorf("invalid handle type")
	}
	// Attempt to clean up docker container if it's left lingering
	// In the current model, sandbox commands spin up ephemeral containers,
	// so terminating is just deleting the host directory.
	return os.RemoveAll(dh.sandboxPath)
}
