package daemon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRepoContextPresent(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "AGENT.md"), []byte("  Run `go test ./...`. Do not touch vendor/.  "), 0o644); err != nil {
		t.Fatal(err)
	}
	got := repoContext(dir)
	if got != "Run `go test ./...`. Do not touch vendor/." {
		t.Errorf("repoContext = %q", got)
	}
}

func TestRepoContextAbsentIsNoOp(t *testing.T) {
	if got := repoContext(t.TempDir()); got != "" {
		t.Errorf("expected empty context when AGENT.md absent, got %q", got)
	}
}

func TestRepoContextTruncated(t *testing.T) {
	dir := t.TempDir()
	big := strings.Repeat("x", maxAgentMDBytes+500)
	if err := os.WriteFile(filepath.Join(dir, "AGENT.md"), []byte(big), 0o644); err != nil {
		t.Fatal(err)
	}
	if len(repoContext(dir)) > maxAgentMDBytes {
		t.Errorf("context should be capped at %d bytes", maxAgentMDBytes)
	}
}

func TestWithRepoContext(t *testing.T) {
	if got := withRepoContext("do X", ""); got != "do X" {
		t.Errorf("empty context must return the description unchanged, got %q", got)
	}
	got := withRepoContext("do X", "conventions")
	if !strings.Contains(got, "AGENT.md") || !strings.Contains(got, "conventions") || !strings.Contains(got, "do X") {
		t.Errorf("combined prompt missing parts: %q", got)
	}
	// Task must come after the context so the Actor sees conventions first.
	if strings.Index(got, "conventions") > strings.Index(got, "do X") {
		t.Error("repo context should precede the task")
	}
}
