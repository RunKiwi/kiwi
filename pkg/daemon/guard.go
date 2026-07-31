package daemon

import (
	"path"
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

// A task that edits one of these is not refused. It used to be: the reasoning
// was that the install phase runs before the Actor, so a package added
// afterwards was never fetched and offline verification would fail on a missing
// module however correct the edit.
//
// The premise held, the conclusion did not. It meant only that the install
// phase had to run again, which it now does — see manifestFingerprint and the
// re-install in executeTask's runTest. The refusal made ordinary work
// impossible: "add a cookie consent banner, use a library if there is one" was
// rejected before the model was called once.

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
