package daemon

import (
	"os"
	"path/filepath"
	"testing"
)

func repo(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// RunKiwi/website: a Next.js project that declares no runtime beyond its
// lockfile. Every test command in it used to run inside golang:1.21-alpine,
// where npm does not exist, so no command could ever pass.
func TestInferImage_NodeProjectWithNoDeclaredVersion(t *testing.T) {
	dir := repo(t, map[string]string{
		"package.json":      `{"scripts":{"build":"next build"}}`,
		"package-lock.json": `{}`,
	})
	if got := inferSandboxImage(dir, "npm run build"); got != "node:20-alpine" {
		t.Errorf("got %q, want node:20-alpine", got)
	}
}

// Kiwi's own repository requires Go 1.25 and the sandbox shipped 1.21, so Kiwi
// could not run its own test command:
//
//	go: go.mod requires go >= 1.25.0 (running go 1.21.13)
func TestInferImage_GoVersionComesFromGoMod(t *testing.T) {
	dir := repo(t, map[string]string{"go.mod": "module x\n\ngo 1.25.0\n"})
	if got := inferSandboxImage(dir, "go test ./..."); got != "golang:1.25-alpine" {
		t.Errorf("got %q, want golang:1.25-alpine", got)
	}
}

// The command is the deciding signal for a polyglot repo: both markers are
// present, and only the command says which toolchain is being exercised.
func TestInferImage_CommandDecidesPolyglotRepo(t *testing.T) {
	dir := repo(t, map[string]string{
		"go.mod":       "module x\n\ngo 1.25.0\n",
		"package.json": `{"scripts":{"test":"jest"}}`,
	})
	if got := inferSandboxImage(dir, "npm test"); got != "node:20-alpine" {
		t.Errorf("npm test in a polyglot repo: got %q, want node:20-alpine", got)
	}
	if got := inferSandboxImage(dir, "go test ./..."); got != "golang:1.25-alpine" {
		t.Errorf("go test in a polyglot repo: got %q, want golang:1.25-alpine", got)
	}
}

func TestInferImage_VersionPins(t *testing.T) {
	cases := []struct {
		name  string
		files map[string]string
		cmd   string
		want  string
	}{
		{"nvmrc", map[string]string{"package.json": "{}", ".nvmrc": "v22.3.0\n"}, "npm test", "node:22-alpine"},
		{"engines", map[string]string{"package.json": `{"engines":{"node":">=18"}}`}, "npm test", "node:18-alpine"},
		{"nvmrc beats engines", map[string]string{
			"package.json": `{"engines":{"node":"18"}}`, ".nvmrc": "22",
		}, "npm test", "node:22-alpine"},
		{"symbolic nvmrc falls back", map[string]string{
			"package.json": "{}", ".nvmrc": "lts/iron",
		}, "npm test", "node:20-alpine"},
		{"python-version", map[string]string{
			"pyproject.toml": "", ".python-version": "3.11.7",
		}, "pytest", "python:3.11-slim"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := inferSandboxImage(repo(t, c.files), c.cmd); got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}

// An explicit devcontainer image is the repo's own answer to this exact
// question and outranks everything inferred.
func TestInferImage_DevcontainerWins(t *testing.T) {
	dir := repo(t, map[string]string{
		"go.mod": "module x\n\ngo 1.25.0\n",
		".devcontainer/devcontainer.json": `{
  // comments are legal here
  "image": "ghcr.io/acme/dev:1"
}`,
	})
	if got := inferSandboxImage(dir, "go test ./..."); got != "ghcr.io/acme/dev:1" {
		t.Errorf("got %q, want the devcontainer image", got)
	}
}

// A devcontainer that builds from a Dockerfile describes a build we do not
// perform; fall through rather than invent its result.
func TestInferImage_DevcontainerWithoutImageIsIgnored(t *testing.T) {
	dir := repo(t, map[string]string{
		"go.mod":                          "module x\n\ngo 1.25.0\n",
		".devcontainer/devcontainer.json": `{"build":{"dockerfile":"Dockerfile"}}`,
	})
	if got := inferSandboxImage(dir, "go test ./..."); got != "golang:1.25-alpine" {
		t.Errorf("got %q, want the inferred image", got)
	}
}

func TestInferImage_MarkersWithoutAUsefulCommand(t *testing.T) {
	cases := []struct{ marker, want string }{
		{"Cargo.toml", rustImage},
		{"pom.xml", mavenImage},
		{"build.gradle", gradleImage},
		{"Gemfile", rubyImage},
		{"composer.json", phpImage},
		{"requirements.txt", "python:3.12-slim"},
	}
	for _, c := range cases {
		t.Run(c.marker, func(t *testing.T) {
			// "make test" names no toolchain, so the marker decides.
			if got := inferSandboxImage(repo(t, map[string]string{c.marker: ""}), "make test"); got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}

// There is no user to ask, so an unrecognisable repo still gets a usable image.
func TestInferImage_AlwaysReturnsSomething(t *testing.T) {
	if got := inferSandboxImage(repo(t, map[string]string{"README.md": "hi"}), ""); got == "" {
		t.Error("inference must never return an empty image")
	}
}

func TestLeadingCommand(t *testing.T) {
	cases := map[string]string{
		"npm test":            "npm",
		"CI=true npm test":    "npm",
		"go test ./...":       "go",
		"./gradlew test":      "gradlew",
		"/usr/bin/pytest -q":  "pytest",
		"FOO=1 BAR=2 cargo t": "cargo",
		"":                    "",
	}
	for in, want := range cases {
		if got := leadingCommand(in); got != want {
			t.Errorf("leadingCommand(%q) = %q, want %q", in, got, want)
		}
	}
}
