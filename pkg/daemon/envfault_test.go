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

// An alpine image carries no C compiler, so a build that needs cgo fails with
// output about gcc — nothing an Actor can fix by editing Go. Both wordings the
// toolchain has used are here: Go < 1.21 says "cgo: exec gcc", later versions
// name the compiler in quotes.
func TestClassifyEnv_MissingCCompiler(t *testing.T) {
	for _, out := range []string{
		"# runtime/cgo\ncgo: exec gcc: exec: \"gcc\": executable file not found in $PATH\nFAIL\trepro [build failed]",
		"# runtime/cgo\ncgo: C compiler \"gcc\" not found: exec: \"gcc\": executable file not found in $PATH",
		"go: -race requires cgo; enable cgo by setting CGO_ENABLED=1",
		"error: linker `cc` not found",
	} {
		f := classifyEnvOutput(out)
		if f == nil {
			t.Fatalf("expected an environment fault for %q", out)
		}
		if f.Kind != "missing_c_toolchain" {
			t.Errorf("kind for %q: got %q, want missing_c_toolchain", out, f.Kind)
		}
	}
}

// The repair is a tag swap on the image already in use: same toolchain, same
// version, plus the compiler. Deriving it from the repository instead would
// hand back the alpine tag that just failed.
func TestCorrectedImage_SwapsToAnImageWithACCompiler(t *testing.T) {
	cgoOut := "# runtime/cgo\ncgo: exec gcc: exec: \"gcc\": executable file not found in $PATH"
	for _, tc := range []struct{ current, want string }{
		{"golang:1.19-alpine", "golang:1.19"},
		{"golang:1.25-alpine", "golang:1.25"},
		{"node:20-alpine", "node:20"},
		{"python:3.12-slim", "python:3.12"},
		{"rust:1-alpine", "rust:1"},
	} {
		dir := t.TempDir()
		// A go.mod is present in the usual case and must not drag the
		// correction back to the alpine tag it names.
		if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module x\n\ngo 1.19\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		got, why := correctedImage(tc.current, cgoOut, dir)
		if got != tc.want {
			t.Errorf("from %s: got %q, want %q", tc.current, got, tc.want)
		}
		if why == "" {
			t.Errorf("from %s: a correction should explain itself for the task log", tc.current)
		}
	}
}

// Already on a Debian-based image means the compiler is there and something
// else is wrong, so there is no tag to swap to and nothing to retry.
func TestCorrectedImage_NoCToolchainSwapWhenAlreadyDebian(t *testing.T) {
	got, _ := correctedImage("golang:1.25", "cgo: exec gcc: exec: \"gcc\": executable file not found in $PATH", "")
	if got != "" {
		t.Errorf("got %q, want no correction when the image already ships gcc", got)
	}
}

// An image we did not choose has no variant we can name: stripping "-alpine"
// off a custom devcontainer image invents a tag that may not exist, and the
// correction is carried into verification even when the retry cannot run.
func TestCorrectedImage_UnknownImageIsNotGuessedAt(t *testing.T) {
	got, _ := correctedImage("mycorp/ci-alpine", "cgo: exec gcc: exec: \"gcc\": executable file not found in $PATH", "")
	if got != "" {
		t.Errorf("got %q, want no correction for an image we do not publish", got)
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
