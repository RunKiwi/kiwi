package infra

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/ibreakthecloud/kiwi/pkg/sandbox"
	"github.com/ibreakthecloud/kiwi/pkg/store"
)

func TestDockerInfraLifecycle(t *testing.T) {
	d := NewDockerInfra(t.TempDir())

	m := &store.Manifest{
		OrgID: "test-org",
		Content: map[string]interface{}{
			"docker_image": "alpine:latest",
		},
	}

	tempDir := t.TempDir()

	ctx := context.Background()
	handle, err := d.Provision(ctx, tempDir, m)
	if err != nil {
		t.Fatalf("Provision failed: %v", err)
	}

	if handle.ID() == "" {
		t.Error("expected non-empty handle ID")
	}

	status, err := d.Status(ctx, handle)
	if err != nil {
		t.Errorf("Status failed: %v", err)
	}
	if status != "RUNNING" {
		t.Errorf("expected RUNNING status, got %s", status)
	}

	output, err := handle.RunCommand(ctx, "echo hello", nil)
	if err != nil {
		t.Fatalf("RunCommand failed: %v", err)
	}
	if !strings.Contains(output, "hello") {
		t.Errorf("expected 'hello' in output, got: %s", output)
	}

	err = d.Terminate(ctx, handle)
	if err != nil {
		t.Fatalf("Terminate failed: %v", err)
	}
}

func TestDockerInfraLimitsApplied(t *testing.T) {
	if os.Getenv("USE_DOCKER") != "true" {
		t.Skip("skipping docker-gated test since USE_DOCKER is not true")
	}

	d := NewDockerInfra(t.TempDir())

	m := &store.Manifest{
		OrgID: "test-org",
		Content: map[string]interface{}{
			"docker_image": "alpine:latest",
		},
	}

	tempDir := t.TempDir()

	cfg := &sandbox.SandboxConfig{
		UseDocker:   true,
		DockerImage: "alpine:latest",
		MemoryLimit: "256m",
		CPULimit:    "0.5",
		NetworkNone: true,
	}
	ctx := context.WithValue(context.Background(), sandbox.SandboxConfigKey, cfg)

	handle, err := d.Provision(ctx, tempDir, m)
	if err != nil {
		t.Fatalf("Provision failed: %v", err)
	}
	defer d.Terminate(ctx, handle)

	// Network should be disabled
	_, err = handle.RunCommand(ctx, "ping -c 1 8.8.8.8", nil)
	if err == nil {
		t.Errorf("expected network to be disabled (ping should fail)")
	}
}
