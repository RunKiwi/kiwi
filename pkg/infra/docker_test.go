package infra

import (
	"context"
	"strings"
	"testing"

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

	output, err := handle.RunCommand(ctx, "echo hello")
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
