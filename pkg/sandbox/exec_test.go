package sandbox

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSandboxDrivers is a contract test that runs against any sandbox driver
// (Docker or Firecracker). It verifies that basic execution, output capture,
// and environment passing works.
func TestSandboxDrivers(t *testing.T) {
	drivers := []struct {
		name    string
		sandbox string
	}{
		{"Local", ""},
		{"Docker", "docker"},
		// Firecracker requires KVM, so we skip it by default in standard CI tests
		// unless explicitly enabled via environment or build tag.
	}

	for _, d := range drivers {
		t.Run(d.name, func(t *testing.T) {
			if d.sandbox == "docker" && os.Getenv("CI") != "" {
				t.Skip("Skipping docker test in CI if daemon unavailable")
			}

			// Setup test workspace
			tmpDir := t.TempDir()
			os.WriteFile(filepath.Join(tmpDir, "hello.txt"), []byte("world\n"), 0644)

			// Setup environment
			os.Setenv("KIWI_SANDBOX", d.sandbox)
			if d.sandbox == "docker" {
				os.Setenv("USE_DOCKER", "true")
			} else {
				os.Setenv("USE_DOCKER", "false")
			}
			defer os.Unsetenv("KIWI_SANDBOX")
			defer os.Unsetenv("USE_DOCKER")

			ctx := context.Background()

			// Test 1: Basic echo
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

			// Test 2: File reading
			res, err = RunCommand(ctx, tmpDir, "cat hello.txt", nil)
			if err != nil {
				t.Fatalf("RunCommand failed: %v", err)
			}
			if !strings.Contains(res.Output, "world") {
				t.Errorf("unexpected output: %q", res.Output)
			}

			// Test 3: Environment variables
			res, err = RunCommand(ctx, tmpDir, "echo $MY_VAR", []string{"MY_VAR=secret123"})
			if err != nil {
				t.Fatalf("RunCommand failed: %v", err)
			}
			if !strings.Contains(res.Output, "secret123") {
				t.Errorf("unexpected output: %q", res.Output)
			}
		})
	}
}

func TestBuildDockerArgs_Runtime(t *testing.T) {
	tests := []struct {
		name    string
		cfg     *SandboxConfig
		wantArg string
	}{
		{
			name: "runsc requested",
			cfg: &SandboxConfig{
				Runtime: "runsc",
			},
			wantArg: "--runtime runsc",
		},
		{
			name:    "no runtime requested",
			cfg:     &SandboxConfig{},
			wantArg: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args, envFile, err := buildDockerArgs("/tmp/test", "ls", nil, tt.cfg, "alpine")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if envFile != "" {
				defer os.Remove(envFile)
			}

			argsStr := strings.Join(args, " ")
			if tt.wantArg != "" && !strings.Contains(argsStr, tt.wantArg) {
				t.Errorf("expected args to contain %q, got %q", tt.wantArg, argsStr)
			}
			if tt.wantArg == "" && strings.Contains(argsStr, "--runtime") {
				t.Errorf("expected no --runtime flag, got %q", argsStr)
			}
		})
	}
}
