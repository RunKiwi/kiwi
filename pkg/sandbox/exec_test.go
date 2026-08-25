package sandbox

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

// The sandbox that runs untrusted, model-generated code must have no network,
// so that code can neither exfiltrate the repo nor reach the LLM key or the
// host's cloud-metadata endpoint. This locks that invariant: NetworkNone must
// translate to `--network none`, and its absence must never silently add
// network access.
func TestBuildDockerArgs_NetworkNone(t *testing.T) {
	on, envFile, err := buildDockerArgs("/tmp/test", "ls", nil, &SandboxConfig{NetworkNone: true}, "alpine", "kiwi-sbx-test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if envFile != "" {
		defer os.Remove(envFile)
	}
	if !strings.Contains(strings.Join(on, " "), "--network none") {
		t.Errorf("NetworkNone=true must add `--network none`, got %q", strings.Join(on, " "))
	}

	off, envFile2, err := buildDockerArgs("/tmp/test", "ls", nil, &SandboxConfig{}, "alpine", "kiwi-sbx-test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if envFile2 != "" {
		defer os.Remove(envFile2)
	}
	if strings.Contains(strings.Join(off, " "), "--network") {
		t.Errorf("NetworkNone=false must not set any --network flag, got %q", strings.Join(off, " "))
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
			args, envFile, err := buildDockerArgs("/tmp/test", "ls", nil, tt.cfg, "alpine", "kiwi-sbx-test")
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

// The heart of the cold-start measurement: Docker reports the Go zero time
// for a container that has not started yet, and that must read as "not
// started" — not as a real (garbage) instant a naive parse would accept.
func TestParseContainerStartedAt(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want bool
	}{
		{"not started yet", "0001-01-01T00:00:00Z\n", false},
		{"real timestamp", "2026-08-25T12:00:00.123456789Z\n", true},
		{"malformed", "not-a-timestamp", false},
		{"empty", "", false},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			_, ok := parseContainerStartedAt(tt.raw)
			if ok != tt.want {
				t.Errorf("parseContainerStartedAt(%q) ok = %v, want %v", tt.raw, ok, tt.want)
			}
		})
	}
}

func TestContainerName(t *testing.T) {
	a, err := containerName()
	if err != nil {
		t.Fatalf("containerName: %v", err)
	}
	b, err := containerName()
	if err != nil {
		t.Fatalf("containerName: %v", err)
	}
	if !strings.HasPrefix(a, "kiwi-sbx-") {
		t.Errorf("containerName() = %q, want kiwi-sbx- prefix", a)
	}
	if a == b {
		t.Errorf("two calls returned the same name %q — container names must be unique across concurrent orgs on the shared fleet", a)
	}
}

// TestWatchProvisioning exercises the full provisioning watcher with a stub
// Docker CLI, verifying that a valid State.StartedAt causes Result.ProvisionMs
// to be positive, while an unstarted or removed container returns zero.
func TestWatchProvisioning(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping watchProvisioning test in short mode")
	}

	// Create a temporary directory for our stub docker script
	tmpDir := t.TempDir()
	stubDockerPath := filepath.Join(tmpDir, "docker")

	tests := []struct {
		name         string
		stubScript   string
		wantPositive bool
		wantZero     bool
	}{
		{
			name: "container started",
			stubScript: `#!/bin/sh
if [ "$1" = "inspect" ] && [ "$2" = "--format" ]; then
    echo "2026-08-25T12:00:00.123456789Z"
    exit 0
fi
exit 1
`,
			wantPositive: true,
		},
		{
			name: "container not started yet",
			stubScript: `#!/bin/sh
if [ "$1" = "inspect" ] && [ "$2" = "--format" ]; then
    echo "0001-01-01T00:00:00Z"
    exit 0
fi
exit 1
`,
			wantZero: true,
		},
		{
			name: "container removed",
			stubScript: `#!/bin/sh
if [ "$1" = "inspect" ] && [ "$2" = "--format" ]; then
    exit 1
fi
exit 1
`,
			wantZero: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Write the stub script
			if err := os.WriteFile(stubDockerPath, []byte(tt.stubScript), 0755); err != nil {
				t.Fatalf("failed to write stub docker: %v", err)
			}

			// Temporarily prepend our stub to PATH
			origPath := os.Getenv("PATH")
			os.Setenv("PATH", tmpDir+":"+origPath)
			defer os.Setenv("PATH", origPath)

			// Call watchProvisioning with callStart far in the past so the stub's
			// fixed timestamp will definitely be after it
			ctx := context.Background()
			callStart := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
			provisionMs := watchProvisioning(ctx, "test-container", callStart)

			if tt.wantPositive && provisionMs <= 0 {
				t.Errorf("expected positive ProvisionMs, got %d", provisionMs)
			}
			if tt.wantZero && provisionMs != 0 {
				t.Errorf("expected zero ProvisionMs, got %d", provisionMs)
			}
		})
	}
}
