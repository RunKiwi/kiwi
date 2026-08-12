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
