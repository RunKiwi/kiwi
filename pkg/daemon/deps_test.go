package daemon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func depsRepo(t *testing.T, names ...string) string {
	t.Helper()
	dir := t.TempDir()
	for _, n := range names {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("{}"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// RunKiwi/website: package.json plus package-lock.json, so the reproducible
// install is npm ci. Without a phase that can run it, --network none makes
// every test command in that repo fail on missing modules.
func TestInferInstall_NpmCiFromLockfile(t *testing.T) {
	step := inferInstallStep(depsRepo(t, "package.json", "package-lock.json"))
	if step == nil {
		t.Fatal("expected an install step")
	}
	if step.Command != "npm ci" {
		t.Errorf("got %q, want npm ci", step.Command)
	}
}

// The lockfile identifies the package manager — yarn.lock and package-lock.json
// describe the same package.json, and running the wrong one is a hard failure.
func TestInferInstall_LockfileDecidesThePackageManager(t *testing.T) {
	cases := []struct {
		files []string
		want  string
	}{
		{[]string{"package.json", "pnpm-lock.yaml"}, "pnpm install --frozen-lockfile"},
		{[]string{"package.json", "yarn.lock"}, "yarn install --frozen-lockfile"},
		{[]string{"package.json", "package-lock.json"}, "npm ci"},
		{[]string{"package.json"}, "npm install"}, // nothing to be reproducible against
	}
	for _, c := range cases {
		t.Run(c.want, func(t *testing.T) {
			step := inferInstallStep(depsRepo(t, c.files...))
			if step == nil || step.Command != c.want {
				t.Errorf("got %+v, want %q", step, c.want)
			}
		})
	}
}

// Python projects using poetry or pipenv also carry a pyproject.toml that pip
// would treat differently, so the more specific tool has to win.
func TestInferInstall_PythonToolPrecedence(t *testing.T) {
	step := inferInstallStep(depsRepo(t, "pyproject.toml", "poetry.lock"))
	if step == nil || !strings.HasPrefix(step.Command, "poetry") {
		t.Errorf("got %+v, want poetry to win over bare pyproject.toml", step)
	}

	step = inferInstallStep(depsRepo(t, "pyproject.toml"))
	if step == nil || step.Command != "pip install -e ." {
		t.Errorf("got %+v, want pip install -e .", step)
	}
}

func TestInferInstall_OtherEcosystems(t *testing.T) {
	cases := map[string]string{
		"go.mod":        "go mod download",
		"Cargo.toml":    "cargo fetch",
		"Gemfile":       "bundle install",
		"composer.json": "composer install --no-interaction",
		"pom.xml":       "mvn -q -B dependency:go-offline",
		"build.gradle":  "gradle --no-daemon dependencies",
	}
	for marker, want := range cases {
		t.Run(marker, func(t *testing.T) {
			step := inferInstallStep(depsRepo(t, marker))
			if step == nil || step.Command != want {
				t.Errorf("got %+v, want %q", step, want)
			}
		})
	}
}

// A repository that declares no dependencies gets no networked phase at all —
// the narrower the exposure, the better.
func TestInferInstall_NoManifestMeansNoNetworkedPhase(t *testing.T) {
	if step := inferInstallStep(depsRepo(t, "README.md", "main.c")); step != nil {
		t.Errorf("got %+v, want no install step", step)
	}
}

// The step records which file decided it, so a surprising install command can
// be traced from the task log rather than guessed at.
func TestInferInstall_RecordsItsSource(t *testing.T) {
	step := inferInstallStep(depsRepo(t, "package.json", "yarn.lock"))
	if step == nil || step.Source != "yarn.lock" {
		t.Errorf("got %+v, want the deciding file recorded", step)
	}
}

func TestInstallTimeout_DefaultAndOverride(t *testing.T) {
	if got := installTimeout(); got != defaultInstallTimeout {
		t.Errorf("default: got %v, want %v", got, defaultInstallTimeout)
	}
	t.Setenv("KIWI_INSTALL_TIMEOUT", "90s")
	if got := installTimeout(); got != 90*time.Second {
		t.Errorf("override: got %v, want 90s", got)
	}
	t.Setenv("KIWI_INSTALL_TIMEOUT", "nonsense")
	if got := installTimeout(); got != defaultInstallTimeout {
		t.Errorf("bad override must fall back, got %v", got)
	}
}

func TestOutputTail(t *testing.T) {
	if got := outputTail("short", 400); got != "short" {
		t.Errorf("got %q, want the whole string", got)
	}
	long := strings.Repeat("x", 500) + "THE REASON"
	got := outputTail(long, 20)
	if !strings.HasSuffix(got, "THE REASON") {
		t.Errorf("the tail must keep the end, got %q", got)
	}
	// Multi-byte input must not be cut mid-rune.
	if got := outputTail(strings.Repeat("é", 100), 15); !utf8ValidString(got) {
		t.Errorf("tail is not valid UTF-8: %q", got)
	}
}

func utf8ValidString(s string) bool {
	for _, r := range s {
		if r == '�' {
			return false
		}
	}
	return true
}
