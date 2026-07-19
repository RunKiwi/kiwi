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
