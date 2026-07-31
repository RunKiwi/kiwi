package daemon

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ibreakthecloud/kiwi/pkg/sandbox"
)

// These run the install phase for real, with USE_DOCKER=false so the command
// executes locally instead of needing a container in CI.

func installEnv(t *testing.T) {
	t.Helper()
	t.Setenv("USE_DOCKER", "false")
}

// The claim this whole split rests on: the phase that has network holds no
// secrets. A dependency's postinstall hook runs arbitrary code with outbound
// access, so it must have nothing worth exfiltrating — not the org's git token,
// not a registry credential, and not the LLM keys already withheld from every
// sandbox.
//
// What this test pins is that the install phase *injects* nothing: it is handed
// a nil environment, where verification is handed an explicit list built from
// the sealed bundle. Two things complete the property and are worth stating
// because the test cannot show them:
//
//   - In production (USE_DOCKER unset) the container inherits nothing at all —
//     no --env-file is written for a nil env, so only the image's own
//     environment exists.
//   - The daemon never places customer credentials in its own process
//     environment; the opened bundle lives in a map. So even the local dev path
//     here, where a child process does inherit, has nothing to inherit.
func TestInstallPhase_PassesNoCredentials(t *testing.T) {
	installEnv(t)
	dir := t.TempDir()
	out := filepath.Join(dir, "leaked.txt")

	// Anything the phase was handed would show up here.
	step := &installStep{
		Command: `printf '%s|%s|%s' "$GIT_TOKEN" "$ANTHROPIC_API_KEY" "$GEMINI_API_KEY" > ` + out,
		Source:  "test",
	}

	d := &Daemon{}
	cfg := &sandbox.SandboxConfig{NetworkNone: true}
	if detail, ok := d.installDependencies(context.Background(), dir, cfg, step, "task-1", nil); !ok {
		t.Fatalf("install failed: %s", detail)
	}

	got, err := os.ReadFile(out)
	if err != nil {
		t.Fatalf("probe did not run: %v", err)
	}
	if string(got) != "||" {
		t.Errorf("the networked phase received credentials: %q", string(got))
	}
}

// Network is enabled for the install and must not leak into verification, where
// model-generated code runs. The caller's config is shared with that phase, so
// a mutation here would silently disable default-deny for the whole task.
func TestInstallPhase_DoesNotEnableNetworkForVerification(t *testing.T) {
	installEnv(t)
	dir := t.TempDir()

	d := &Daemon{}
	cfg := &sandbox.SandboxConfig{NetworkNone: true, DockerImage: "node:20-alpine"}
	if _, ok := d.installDependencies(context.Background(), dir, cfg,
		&installStep{Command: "true", Source: "test"}, "task-1", nil); !ok {
		t.Fatal("install should have succeeded")
	}

	if !cfg.NetworkNone {
		t.Error("verification lost its default-deny networking to the install phase")
	}
}

// A failed install stops the task with the reason attached. Letting the loop
// discover it instead hands the Actor "Cannot find module 'react'" — something
// it will earnestly try to fix by editing code, burning the whole budget to
// learn that it cannot.
func TestInstallPhase_FailureStopsTheTaskWithAReason(t *testing.T) {
	installEnv(t)
	dir := t.TempDir()

	d := &Daemon{}
	cfg := &sandbox.SandboxConfig{NetworkNone: true}
	detail, ok := d.installDependencies(context.Background(), dir, cfg,
		&installStep{Command: `echo "npm ERR! 404 Not Found: left-pad@9.9.9" >&2; exit 1`, Source: "package-lock.json"}, "task-1", nil)

	if ok {
		t.Fatal("expected the install to fail")
	}
	if !strings.Contains(detail, "dependency installation failed") {
		t.Errorf("detail should say what phase failed, got %q", detail)
	}
	if !strings.Contains(detail, "404 Not Found") {
		t.Errorf("detail should carry the reason, got %q", detail)
	}
	if len([]rune(detail)) > maxDetailLen+len("…(truncated)") {
		t.Errorf("detail is unbounded (%d runes)", len([]rune(detail)))
	}
}

// The install is the first command to run, so a wrong image usually surfaces
// here — and the correction has to carry into verification, or the tests
// rediscover the identical fault.
func TestInstallPhase_ImageCorrectionCarriesIntoVerification(t *testing.T) {
	installEnv(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}

	d := &Daemon{}
	cfg := &sandbox.SandboxConfig{NetworkNone: true, DockerImage: "golang:1.25-alpine"}

	// Fails once with a missing runtime, then succeeds — so the retry, and the
	// image it retried with, are both observable.
	step := &installStep{
		Command: `if [ ! -f ran ]; then touch ran; echo "sh: npm: not found" >&2; exit 127; fi`,
		Source:  "package.json",
	}

	if _, ok := d.installDependencies(context.Background(), dir, cfg, step, "task-1", nil); !ok {
		t.Fatal("the retry should have succeeded")
	}

	if cfg.DockerImage != "node:20-alpine" {
		t.Errorf("correction did not reach verification: image is still %q", cfg.DockerImage)
	}
}
