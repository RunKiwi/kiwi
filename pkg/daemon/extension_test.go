package daemon

import (
	"os"
	"path/filepath"
	"testing"
)

// The reported failure: the planner named examples/advanced.rs for a Go
// repository. The Actor may change a file's contents but never its name, so the
// Critic rejected Go code in a .rs file three times — correctly — and the task
// spent its whole ten-minute budget on a position it could not win.
func TestCorrectExtension_RustFileInAGoRepo(t *testing.T) {
	got := correctNewFileExtension("examples/advanced.rs", ecoGo, t.TempDir())
	if got != "examples/advanced.go" {
		t.Errorf("got %q, want examples/advanced.go", got)
	}
}

func TestCorrectExtension_AcrossEcosystems(t *testing.T) {
	dir := t.TempDir()
	cases := []struct {
		in   string
		eco  ecosystem
		want string
	}{
		{"src/thing.go", ecoRust, "src/thing.rs"},
		{"lib/thing.py", ecoGo, "lib/thing.go"},
		{"app/thing.rb", ecoPython, "app/thing.py"},
		{"src/Thing.java", ecoGo, "src/Thing.go"},
		{"src/thing.ts", ecoGo, "src/thing.go"},
	}
	for _, c := range cases {
		if got := correctNewFileExtension(c.in, c.eco, dir); got != c.want {
			t.Errorf("correctNewFileExtension(%q, %s) = %q, want %q", c.in, c.eco, got, c.want)
		}
	}
}

// The damaging mistake is over-correcting. A Go project legitimately contains
// documentation, config and data files, and renaming those would be a worse bug
// than the one being fixed.
func TestCorrectExtension_LeavesNonSourceFilesAlone(t *testing.T) {
	dir := t.TempDir()
	for _, f := range []string{
		"README.md",
		"config/app.yaml",
		"data/fixtures.json",
		"Dockerfile",
		"Makefile",
		".gitignore",
		"docs/design.txt",
	} {
		if got := correctNewFileExtension(f, ecoGo, dir); got != f {
			t.Errorf("%q was rewritten to %q; only unambiguous source extensions may be corrected", f, got)
		}
	}
}

// A file already in the repository's own language is correct by definition.
func TestCorrectExtension_MatchingExtensionIsUntouched(t *testing.T) {
	dir := t.TempDir()
	if got := correctNewFileExtension("cmd/main.go", ecoGo, dir); got != "cmd/main.go" {
		t.Errorf("got %q, want the path unchanged", got)
	}
}

// Node is the one ecosystem where two extensions are both right, so the
// repository decides.
func TestCorrectExtension_NodeFollowsTheProject(t *testing.T) {
	js := t.TempDir()
	if got := correctNewFileExtension("src/thing.go", ecoNode, js); got != "src/thing.js" {
		t.Errorf("plain JS project: got %q, want src/thing.js", got)
	}

	ts := t.TempDir()
	if err := os.WriteFile(filepath.Join(ts, "tsconfig.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := correctNewFileExtension("src/thing.go", ecoNode, ts); got != "src/thing.ts" {
		t.Errorf("TypeScript project: got %q, want src/thing.ts", got)
	}
}
