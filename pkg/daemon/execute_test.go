package daemon

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ibreakthecloud/kiwi/pkg/agent"
	"github.com/ibreakthecloud/kiwi/pkg/provider"
)

// fixOnceProvider is a mock Actor that replaces the file with fixedContent on
// its first call — enough to drive one real loop iteration to success.
type fixOnceProvider struct{ fixedContent string }

func (p *fixOnceProvider) GetCodeEdit(ctx context.Context, task, fileName, code, buildOutput string) (string, error) {
	return p.fixedContent, nil
}

// newExecTestDaemon builds a Daemon wired with a mock provider and local (non-
// Docker) sandbox execution, so executeTask can run the real Actor–Critic loop
// end-to-end without a network or Docker.
func newExecTestDaemon(t *testing.T, fixedContent string) *Daemon {
	t.Helper()
	t.Setenv("USE_DOCKER", "false") // run the test command locally, not in Docker
	d, err := New(Config{CacheDir: t.TempDir(), KeyPath: ""})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	d.newProvider = func(creds map[string]string, model string) (provider.Provider, provider.Critic) {
		return &fixOnceProvider{fixedContent: fixedContent}, nil // no critic
	}
	return d
}

func TestExecuteTask_RealLoopFixesFileUntilTestPasses(t *testing.T) {
	// The task: make `go`-free verification pass. We use a trivial shell test
	// that greps the target file for a marker the mock provider will write.
	d := newExecTestDaemon(t, "package x // FIXED\n")

	// The fallback (no-repo) branch puts the workspace under os.TempDir(); point
	// the target file there and seed it in a broken state.
	specID := "loop-task-1"
	workdir := filepath.Join(os.TempDir(), "kiwi-sandbox", specID)
	if err := os.MkdirAll(workdir, 0o755); err != nil {
		t.Fatalf("mkdir workdir: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(workdir) })
	target := filepath.Join(workdir, "main.go")
	if err := os.WriteFile(target, []byte("package x // broken\n"), 0o644); err != nil {
		t.Fatalf("seed target: %v", err)
	}

	spec := agent.WorkerSpec{
		ID:      specID,
		Model:   "sonnet",
		Task:    "add the FIXED marker",
		File:    "main.go",
		TestCmd: "grep -q FIXED main.go",
	}
	creds := map[string]string{"ANTHROPIC_API_KEY": "test-key"} // makes newProvider return the mock

	ok, _, _ := d.executeTask(context.Background(), spec, creds)
	if !ok {
		t.Fatal("executeTask returned false; expected the loop to fix the file and pass the test")
	}
	// The Actor's edit must be on disk.
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read target: %v", err)
	}
	if want := "package x // FIXED\n"; string(got) != want {
		t.Errorf("file = %q, want %q", got, want)
	}
}

func TestExecuteTask_SmokeFallbackWhenNoTestCmd(t *testing.T) {
	// No test command and no provider key: executeTask must not pretend to run
	// an agent; it falls back to the smoke command and still succeeds.
	t.Setenv("USE_DOCKER", "false")
	d, err := New(Config{CacheDir: t.TempDir(), KeyPath: ""})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	specID := "smoke-task-1"
	spec := agent.WorkerSpec{ID: specID, Task: "just smoke"}
	t.Cleanup(func() { os.RemoveAll(filepath.Join(os.TempDir(), "kiwi-sandbox", specID)) })

	if ok, _, _ := d.executeTask(context.Background(), spec, map[string]string{}); !ok {
		t.Fatal("smoke fallback should succeed")
	}
}

func TestExecuteTask_FailsWhenTestNeverPasses(t *testing.T) {
	// The provider writes content that never satisfies the test; the loop must
	// exhaust and executeTask must report failure (no false-green).
	d := newExecTestDaemon(t, "package x // still broken\n")

	specID := "loop-task-fail"
	workdir := filepath.Join(os.TempDir(), "kiwi-sandbox", specID)
	if err := os.MkdirAll(workdir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(workdir) })
	if err := os.WriteFile(filepath.Join(workdir, "main.go"), []byte("broken\n"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}

	spec := agent.WorkerSpec{
		ID:      specID,
		Task:    "impossible",
		File:    "main.go",
		TestCmd: "grep -q FIXED main.go",
	}
	ok, _, detail := d.executeTask(context.Background(), spec, map[string]string{"ANTHROPIC_API_KEY": "k"})
	if ok {
		t.Fatal("expected failure when the test never passes")
	}
	// A FAILED task must explain itself (result_detail), not report an empty reason.
	if detail == "" {
		t.Error("expected a non-empty failure detail so the FAILED task explains itself")
	}
}

func TestTruncateDetail(t *testing.T) {
	if got := truncateDetail("short"); got != "short" {
		t.Errorf("short string should pass through, got %q", got)
	}
	long := strings.Repeat("x", maxDetailLen+50)
	got := truncateDetail(long)
	if !strings.HasSuffix(got, "…(truncated)") {
		t.Errorf("over-long string should be marked truncated, got suffix %q", got[len(got)-20:])
	}
	if len([]rune(got)) != maxDetailLen+len([]rune("…(truncated)")) {
		t.Errorf("truncated length = %d runes, want %d", len([]rune(got)), maxDetailLen+len([]rune("…(truncated)")))
	}
}

func TestExecuteTask_FileScope(t *testing.T) {
	d := newExecTestDaemon(t, "")

	tests := []struct {
		name  string
		file  string
		valid bool
	}{
		{"benign_simple", "main.go", true},
		{"benign_nested", "pkg/main.go", true},
		{"malicious_abs", "/etc/passwd", false},
		{"malicious_dotdot", "../main.go", false},
		{"malicious_nested_dotdot", "pkg/../../main.go", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			spec := agent.WorkerSpec{
				ID:   "task-" + tc.name,
				File: tc.file,
			}
			ok, _, detail := d.executeTask(context.Background(), spec, nil)
			if tc.valid && detail == "file path escapes worktree" {
				t.Errorf("expected valid path %q to not be rejected", tc.file)
			}
			if !tc.valid && detail != "file path escapes worktree" {
				t.Errorf("expected malicious path %q to be rejected, got %v (ok=%v)", tc.file, detail, ok)
			}
		})
	}
}
