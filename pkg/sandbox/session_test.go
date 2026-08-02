package sandbox

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The local path is what CI and Docker-less development hosts exercise, so it
// has to be a real implementation rather than a stub that only compiles.
func TestSessionExecutesInTheWorkspace(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("kiwi"), 0o644); err != nil {
		t.Fatal(err)
	}

	s, err := NewSession(context.Background(), dir, &SandboxConfig{UseDocker: false}, SessionOpts{})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	defer s.Close()

	if !s.Local() {
		t.Fatal("expected a local session when UseDocker is false")
	}

	res, err := s.Exec(context.Background(), "cat hello.txt", nil)
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	if !res.Success {
		t.Fatalf("expected success, got output %q", res.Output)
	}
	if strings.TrimSpace(res.Output) != "kiwi" {
		t.Errorf("output = %q, want %q", res.Output, "kiwi")
	}
}

// State written by one command must be visible to the next. This is the whole
// reason a session exists rather than a sequence of RunCommand calls.
func TestSessionPersistsStateBetweenCommands(t *testing.T) {
	dir := t.TempDir()
	s, err := NewSession(context.Background(), dir, &SandboxConfig{UseDocker: false}, SessionOpts{})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	if _, err := s.Exec(context.Background(), "echo written > out.txt", nil); err != nil {
		t.Fatal(err)
	}
	res, err := s.Exec(context.Background(), "cat out.txt", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Success || !strings.Contains(res.Output, "written") {
		t.Fatalf("second command did not see the first's writes: success=%v output=%q", res.Success, res.Output)
	}
}

// A failing command is ordinary feedback for the agent, not an infrastructure
// error — the same distinction loop.TestFunc draws.
func TestSessionReportsCommandFailureWithoutError(t *testing.T) {
	s, err := NewSession(context.Background(), t.TempDir(), &SandboxConfig{UseDocker: false}, SessionOpts{})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	res, err := s.Exec(context.Background(), "exit 3", nil)
	if err != nil {
		t.Fatalf("a non-zero exit must not be returned as an error: %v", err)
	}
	if res.Success {
		t.Fatal("expected Success=false for a non-zero exit")
	}
}

func TestSessionPassesEnvironment(t *testing.T) {
	s, err := NewSession(context.Background(), t.TempDir(), &SandboxConfig{UseDocker: false}, SessionOpts{})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	res, err := s.Exec(context.Background(), "echo $KIWI_TEST_VAR", []string{"KIWI_TEST_VAR=present"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Output, "present") {
		t.Errorf("environment not passed through: %q", res.Output)
	}
}

func TestSessionCloseIsIdempotent(t *testing.T) {
	s, err := NewSession(context.Background(), t.TempDir(), &SandboxConfig{UseDocker: false}, SessionOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("first close: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("second close: %v", err)
	}
}

// Using a closed session must fail loudly. Silently running the command on the
// host would defeat the isolation the session represents.
func TestSessionRejectsExecAfterClose(t *testing.T) {
	s, err := NewSession(context.Background(), t.TempDir(), &SandboxConfig{UseDocker: false}, SessionOpts{})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Exec(context.Background(), "echo hi", nil); err == nil {
		t.Fatal("expected Exec on a closed session to fail")
	}
}
