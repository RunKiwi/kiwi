package daemon

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/ibreakthecloud/kiwi/pkg/provider"
)

// providerNameForModel returns the human-readable provider a model routes to.
// It resolves through the same provider.ProviderOf that defaultProvider uses, so
// the message naming the missing key can never name a different provider than
// the one whose key was actually looked up.
func providerNameForModel(model string) string {
	return provider.DisplayNameFor(provider.ProviderOf(model))
}

// testCmdRequired reports whether a task with no test command (given or
// inferred) must be refused. False only for a task explicitly hinted
// investigation-only — everything else keeps today's behavior exactly,
// including a code-fixing task with no detectable convention.
func testCmdRequired(testCmd string, investigationOnly bool) bool {
	return testCmd == "" && !investigationOnly
}

// noOpVerify is the verification story for a task testCmdRequired let
// through with an empty test command — reachable only when
// spec.InvestigationOnly is true. There is nothing to run, so it reports a
// trivial pass rather than handing an empty command to the sandbox, which
// runInSandbox/sandbox.RunCommand were never built to receive.
func noOpVerify(context.Context) (string, bool, error) { return "", true, nil }

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
		// `python -m pytest` rather than the console script: packages are
		// installed to an explicit PIP_TARGET so they outlive the install
		// container, and that directory is on PYTHONPATH, not PATH.
		return "python -m pytest"
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
