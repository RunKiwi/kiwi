//go:build firecracker_test
// +build firecracker_test

package sandbox

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestFirecrackerDriver runs the contract test against the Firecracker driver.
// It requires /dev/kvm, firecracker binary in PATH, and rootfs/vmlinux at the default paths.
func TestFirecrackerDriver(t *testing.T) {
	if _, err := os.Stat("/dev/kvm"); os.IsNotExist(err) {
		t.Skip("Skipping Firecracker test: /dev/kvm not found")
	}

	tmpDir := t.TempDir()
	os.WriteFile(filepath.Join(tmpDir, "hello.txt"), []byte("world\n"), 0644)

	os.Setenv("KIWI_SANDBOX", "firecracker")
	defer os.Unsetenv("KIWI_SANDBOX")

	ctx := context.Background()
	res, err := RunCommand(ctx, tmpDir, "echo 'hello sandbox'", nil)
	if err != nil {
		t.Fatalf("RunCommand failed: %v", err)
	}
	if !res.Success {
		t.Errorf("expected success, got false")
	}
	if !strings.Contains(res.Output, "hello sandbox") {
		t.Errorf("unexpected output: %q", res.Output)
	}
}
