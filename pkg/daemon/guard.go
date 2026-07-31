package daemon

import (
	"path"
	"path/filepath"
	"strings"
)

// looksLikeTestFile reports whether p is a test file across the languages Kiwi
// targets. It is the "at minimum" anti-gaming heuristic (issue #132): if the
// Actor's target IS a test, it could satisfy the gate by weakening the test
// rather than fixing the code. The check is filename/path based and deliberately
// conservative — false positives (refusing a legitimate test edit) are safer
// than false negatives (letting the agent grade its own homework).
// manifestFiles name a project's dependency set. Editing one changes which
// packages are required, which is the one edit verification cannot cope with.
var manifestFiles = map[string]bool{
	"go.mod": true, "go.sum": true,
	"package.json": true, "package-lock.json": true, "yarn.lock": true, "pnpm-lock.yaml": true,
	"requirements.txt": true, "pyproject.toml": true, "poetry.lock": true, "Pipfile": true, "Pipfile.lock": true,
	"Cargo.toml": true, "Cargo.lock": true,
	"Gemfile": true, "Gemfile.lock": true,
	"composer.json": true, "composer.lock": true,
	"pom.xml": true, "build.gradle": true, "build.gradle.kts": true,
}

// dependencyChangeBlocked reports why a task cannot be completed offline when it
// targets a dependency manifest, or "" when it does not.
//
// The install phase fetches what the manifest asks for *before* the Actor runs.
// If the Actor then changes a version, that package was never downloaded, and
// verification has no network to fetch it — so the build fails on a missing
// module no matter how correct the edit is. Go makes it starker still: go.sum
// needs cryptographic hashes of the new module, which cannot be computed
// without downloading it.
//
// This is inherent to verifying model-generated code without network access,
// not a bug to be fixed downstream. Saying so immediately is the whole value:
// "update the sirupsen/logrus dependency to latest" otherwise spends six Actor
// steps and the user's entire budget proving it.
func dependencyChangeBlocked(files []string) string {
	for _, f := range files {
		if manifestFiles[filepath.Base(filepath.Clean(f))] {
			return "this task changes " + filepath.Base(f) + ", and dependency changes cannot be verified here: " +
				"packages are downloaded before the agent runs, and the verification step has no network access to fetch a version that was added afterwards. " +
				"Run the upgrade locally (e.g. `go get -u`) and let Kiwi handle the code changes it requires."
		}
	}
	return ""
}

func looksLikeTestFile(p string) bool {
	if p == "" {
		return false
	}
	lower := strings.ToLower(strings.ReplaceAll(p, "\\", "/"))
	base := path.Base(lower)

	// A path segment that is a conventional test directory.
	for _, seg := range strings.Split(lower, "/") {
		switch seg {
		case "test", "tests", "__tests__", "spec", "specs":
			return true
		}
	}

	suffixes := []string{
		"_test.go",                                       // Go
		".test.js", ".test.ts", ".test.jsx", ".test.tsx", // JS/TS
		".spec.js", ".spec.ts", ".spec.jsx", ".spec.tsx",
		"_test.py", "_spec.rb", "_test.rb", "_test.exs",
		"test.java", "tests.java", // *Test.java / *Tests.java (lowercased)
	}
	for _, s := range suffixes {
		if strings.HasSuffix(base, s) {
			return true
		}
	}

	// Python convention: test_*.py.
	if strings.HasPrefix(base, "test_") && strings.HasSuffix(base, ".py") {
		return true
	}
	return false
}
