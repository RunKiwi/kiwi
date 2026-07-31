package daemon

import (
	"os"
	"path/filepath"
	"testing"
)

// The exact output that cost a Next.js task its whole budget: npm does not
// exist in a Go image, sh exits 127, and the loop read it as a failing test.
func TestClassifyEnv_MissingRuntime(t *testing.T) {
	f := classifyEnvOutput("sh: npm: not found\n")
	if f == nil {
		t.Fatal("expected an environment fault")
	}
	if f.Kind != "missing_runtime" {
		t.Errorf("kind: got %q, want missing_runtime", f.Kind)
	}
	if f.Image != "node:20-alpine" {
		t.Errorf("image: got %q, want node:20-alpine", f.Image)
	}
}

func TestClassifyEnv_NotFoundPhrasings(t *testing.T) {
	for _, out := range []string{
		"sh: npm: not found",
		"bash: line 1: npm: command not found",
		"/bin/sh: 1: npm: not found",
		"sh: pytest: not found",
		"sh: cargo: not found",
	} {
		if classifyEnvOutput(out) == nil {
			t.Errorf("missed a missing runtime in %q", out)
		}
	}
}

// Kiwi's own failure, and the useful part is that the toolchain states the
// version it needs — so the repair is exact rather than a guess.
func TestClassifyEnv_GoVersionMismatchNamesTheFix(t *testing.T) {
	f := classifyEnvOutput("go: go.mod requires go >= 1.25.0 (running go 1.21.13; GOTOOLCHAIN=local)")
	if f == nil {
		t.Fatal("expected an environment fault")
	}
	if f.Kind != "version_mismatch" {
		t.Errorf("kind: got %q, want version_mismatch", f.Kind)
	}
	if f.Image != "golang:1.25-alpine" {
		t.Errorf("image: got %q, want golang:1.25-alpine", f.Image)
	}
}

// The expensive mistake would be treating a real failure as an environment
// fault: it re-runs the test and could mask the very thing the loop exists to
// fix. Ordinary failing output must classify as nothing at all.
func TestClassifyEnv_RealTestFailuresAreNotFaults(t *testing.T) {
	for _, out := range []string{
		"--- FAIL: TestDivide (0.00s)\n    math_test.go:12: got 0, want 3",
		"Tests: 1 failed, 4 passed",
		"AssertionError: expected true to equal false",
		"./main.go:7:2: declared and not used: x",
		"error: cannot find module 'left-pad'",
		"",
	} {
		if f := classifyEnvOutput(out); f != nil {
			t.Errorf("output %q misclassified as %s", out, f.Kind)
		}
	}
}

// A command we do not recognise going missing is not something an image swap
// can fix, so it is left alone rather than triggering a pointless retry.
func TestClassifyEnv_UnknownMissingCommandIsNotRepairable(t *testing.T) {
	if f := classifyEnvOutput("sh: frobnicate: not found"); f != nil {
		t.Errorf("got %+v, want nil for an unknown tool", f)
	}
}

// The repair picks up a version the repo pins, rather than the default.
func TestCorrectedImage_UsesTheRepositorysPinnedVersion(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".nvmrc"), []byte("22"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, why := correctedImage("golang:1.25-alpine", "sh: npm: not found", dir)
	if got != "node:22-alpine" {
		t.Errorf("got %q, want node:22-alpine", got)
	}
	if why == "" {
		t.Error("a correction should explain itself for the task log")
	}
}

// Already running the image the fault points at means the retry would fail
// identically. Report the failure instead of looping on it.
func TestCorrectedImage_NoSelfRetry(t *testing.T) {
	got, _ := correctedImage("node:20-alpine", "sh: npm: not found", "")
	if got != "" {
		t.Errorf("got %q, want no correction when already on that image", got)
	}
}

func TestCorrectedImage_RealFailureIsNotCorrected(t *testing.T) {
	got, _ := correctedImage("node:20-alpine", "Tests: 1 failed, 4 passed", "")
	if got != "" {
		t.Errorf("got %q, want no correction for a genuine test failure", got)
	}
}
