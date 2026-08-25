package daemon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// This file used to assert the opposite behaviour: a task touching a dependency
// manifest was refused outright, on the reasoning that the install phase runs
// before the Actor and verification has no network, so a package added
// afterwards could never be fetched.
//
// The premise was right; the conclusion was too broad. It did not follow that
// the task was impossible — only that the install phase had to run again. The
// refusal made a whole class of ordinary work unreachable: "add a cookie
// consent banner, use a third-party library if there is one" is a normal
// request, and it was rejected before the model was ever called.
//
// The install phase is now re-run when a manifest changes, so these tests cover
// the mechanism that replaced the refusal.

// The fingerprint is what decides whether a re-install is needed. If it misses
// an edit, verification runs against a package that was never installed and
// fails on a missing module the Actor cannot fix by editing code.
func TestManifestFingerprint_ChangesWhenAManifestChanges(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	write("package.json", `{"dependencies":{}}`)
	before := manifestFingerprint(dir)

	write("package.json", `{"dependencies":{"react-cookie-consent":"^9.0.0"}}`)
	after := manifestFingerprint(dir)

	if before == after {
		t.Error("adding a dependency did not change the fingerprint, so no re-install would run")
	}
}

// The costly false positive: a re-install is minutes, and the Actor rewrites
// whole files, so an unchanged rewrite must not look like a change. This is why
// the fingerprint hashes content rather than reading mtimes.
func TestManifestFingerprint_StableWhenContentIsUnchanged(t *testing.T) {
	dir := t.TempDir()
	body := `{"dependencies":{"react":"^18.0.0"}}`
	path := filepath.Join(dir, "package.json")

	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	first := manifestFingerprint(dir)

	// Rewritten byte-for-byte, as the Actor does when it changes nothing.
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if second := manifestFingerprint(dir); first != second {
		t.Error("an identical rewrite changed the fingerprint; every step would re-install")
	}
}

// Deleting a manifest is a dependency change too.
func TestManifestFingerprint_NoticesRemoval(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "requirements.txt")
	if err := os.WriteFile(path, []byte("requests==2.31.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	before := manifestFingerprint(dir)

	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if manifestFingerprint(dir) == before {
		t.Error("removing a manifest must register as a change")
	}
}

// A repository with no manifests at all is stable and empty — it must not
// trigger an install that has nothing to do.
func TestManifestFingerprint_EmptyRepoIsStable(t *testing.T) {
	a, b := manifestFingerprint(t.TempDir()), manifestFingerprint(t.TempDir())
	if a != b {
		t.Error("two dependency-free repos must fingerprint identically")
	}
}

// The heart of the fix. `npm ci` installs strictly from the lockfile and FAILS
// when package.json disagrees with it — which is exactly the state the Actor
// creates by adding a dependency. Re-running the frozen command would turn a
// working edit into a hard error, so the resolving form is required.
func TestInstallStep_ReinstallDoesNotUseTheFrozenCommand(t *testing.T) {
	cases := []struct {
		lockfile string
		frozen   string // must NOT be used on re-install
	}{
		{"package-lock.json", "npm ci"},
		{"yarn.lock", "--frozen-lockfile"},
		{"pnpm-lock.yaml", "--frozen-lockfile"},
	}
	for _, c := range cases {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte("{}"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, c.lockfile), []byte("{}"), 0o644); err != nil {
			t.Fatal(err)
		}

		locked := installStepFor(dir, true)
		if locked == nil || !strings.Contains(locked.Command, strings.TrimPrefix(c.frozen, "--")) {
			t.Errorf("%s: initial install should be reproducible, got %+v", c.lockfile, locked)
		}

		reinstall := installStepFor(dir, false)
		if reinstall == nil {
			t.Fatalf("%s: no re-install step", c.lockfile)
		}
		if strings.Contains(reinstall.Command, c.frozen) {
			t.Errorf("%s: re-install uses %q, which fails when the lock and manifest disagree — got %q",
				c.lockfile, c.frozen, reinstall.Command)
		}
	}
}

// --legacy-peer-deps unblocks the frozen install (ERESOLVE against a
// lockfile the repo already committed) but must not survive onto the
// resolving install: that call writes the lockfile the PR ships, and a
// lockfile npm resolved by ignoring peer constraints is one a plain
// `npm ci` on the user's own machine can reject.
func TestInstallStep_LegacyPeerDepsOnlyOnFrozenInstall(t *testing.T) {
	for _, lockfile := range []string{"package-lock.json", ""} {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte("{}"), 0o644); err != nil {
			t.Fatal(err)
		}
		if lockfile != "" {
			if err := os.WriteFile(filepath.Join(dir, lockfile), []byte("{}"), 0o644); err != nil {
				t.Fatal(err)
			}
		}

		locked := installStepFor(dir, true)
		if locked == nil || !strings.Contains(locked.Command, "--legacy-peer-deps") {
			t.Errorf("lockfile=%q: frozen install should carry --legacy-peer-deps, got %+v", lockfile, locked)
		}
		reinstall := installStepFor(dir, false)
		if reinstall == nil || strings.Contains(reinstall.Command, "--legacy-peer-deps") {
			t.Errorf("lockfile=%q: resolving install must not carry --legacy-peer-deps, got %+v", lockfile, reinstall)
		}
	}
}

// Go needs the stronger form: `go mod download` fetches what go.mod already
// names, but an edited go.mod needs the new requirement resolved and its go.sum
// hashes written, which only `go mod tidy` does.
func TestInstallStep_GoReinstallTidies(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := installStepFor(dir, true); got == nil || got.Command != "go mod download" {
		t.Errorf("initial install = %+v, want `go mod download`", got)
	}
	if got := installStepFor(dir, false); got == nil || !strings.Contains(got.Command, "tidy") {
		t.Errorf("re-install = %+v, want `go mod tidy`", got)
	}
}

// A dependency-free repository still has no install step in either mode.
func TestInstallStep_NoManifestsMeansNoStep(t *testing.T) {
	dir := t.TempDir()
	if got := installStepFor(dir, true); got != nil {
		t.Errorf("locked: got %+v, want nil", got)
	}
	if got := installStepFor(dir, false); got != nil {
		t.Errorf("resolving: got %+v, want nil", got)
	}
}

// The manifest set is what the fingerprint watches. A missing entry means an
// edit to that file goes unnoticed and the re-install never runs.
func TestManifestSet_CoversEveryEcosystem(t *testing.T) {
	for _, f := range []string{
		"go.mod", "go.sum",
		"package.json", "package-lock.json", "yarn.lock", "pnpm-lock.yaml",
		"requirements.txt", "pyproject.toml", "poetry.lock", "Pipfile.lock",
		"Cargo.toml", "Cargo.lock",
		"Gemfile", "Gemfile.lock",
		"composer.json", "composer.lock",
		"pom.xml", "build.gradle", "build.gradle.kts",
	} {
		if !manifestFiles[f] {
			t.Errorf("%s is a dependency manifest but is not watched", f)
		}
	}
}

// The other direction: ordinary source files must not be treated as manifests,
// or every edit would trigger a five-minute re-install.
func TestManifestSet_ExcludesOrdinaryFiles(t *testing.T) {
	for _, f := range []string{
		"main.go", "handler.go", "README.md",
		"src/components/Footer.tsx", "parse.go",
	} {
		if manifestFiles[filepath.Base(f)] {
			t.Errorf("%s is not a dependency manifest", f)
		}
	}
}
