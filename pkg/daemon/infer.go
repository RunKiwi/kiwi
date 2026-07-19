package daemon

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// providerNameForModel returns the human-readable provider a model routes to,
// mirroring defaultProvider's selection (gemini* → Gemini, else Anthropic). Used
// only for messages, so it never needs a key.
func providerNameForModel(model string) string {
	if strings.HasPrefix(model, "gemini") {
		return "Gemini"
	}
	return "Anthropic"
}

// inferTestCmd guesses a project's test command from marker files at the repo
// root, so a task submitted without an explicit test_cmd can still be verified.
// It returns "" when nothing recognisable is present, leaving the caller to fail
// with a clear "no test command" reason rather than guess wrong.
//
// The order matters only where a repo carries several ecosystems' markers; the
// checks are deliberately conservative — a command we are confident actually
// runs that project's tests.
func inferTestCmd(dir string) string {
	exists := func(name string) bool {
		_, err := os.Stat(filepath.Join(dir, name))
		return err == nil
	}

	switch {
	case exists("go.mod"):
		return "go test ./..."
	case exists("Cargo.toml"):
		return "cargo test"
	case exists("package.json"):
		// Only claim `npm test` when a test script is actually defined —
		// otherwise npm errors, which would look like a failing test.
		if hasNpmTestScript(filepath.Join(dir, "package.json")) {
			return "npm test"
		}
	case exists("pyproject.toml"), exists("setup.py"), exists("pytest.ini"), exists("tox.ini"):
		return "pytest"
	case exists("pom.xml"):
		return "mvn -q -B test"
	case exists("build.gradle"), exists("build.gradle.kts"):
		return "gradle test"
	}

	// A Makefile with a `test:` target is a common language-agnostic entry point.
	if hasMakeTarget(filepath.Join(dir, "Makefile"), "test") {
		return "make test"
	}
	return ""
}

// hasNpmTestScript reports whether package.json defines a scripts.test entry.
func hasNpmTestScript(path string) bool {
	b, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var pkg struct {
		Scripts map[string]string `json:"scripts"`
	}
	if err := json.Unmarshal(b, &pkg); err != nil {
		return false
	}
	return strings.TrimSpace(pkg.Scripts["test"]) != ""
}

// hasMakeTarget reports whether a Makefile declares the given target (a line
// beginning `<target>:`).
func hasMakeTarget(path, target string) bool {
	b, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	prefix := target + ":"
	for _, line := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), prefix) {
			return true
		}
	}
	return false
}
