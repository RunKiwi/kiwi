package daemon

import "testing"

func TestLooksLikeTestFile(t *testing.T) {
	tests := map[string]bool{
		// Test files → true
		"math_utils_test.go":     true,
		"pkg/foo/bar_test.go":    true,
		"src/app.test.ts":        true,
		"src/app.spec.jsx":       true,
		"test_math.py":           true,
		"calc_test.py":           true,
		"user_spec.rb":           true,
		"FooTest.java":           true,
		"tests/integration/x.go": true,
		"__tests__/render.js":    true,
		"spec/models/user.rb":    true,
		// Non-test source → false
		"math_utils.go":          false,
		"src/app.ts":             false,
		"main.py":                false,
		"internal/testdata/x.go": false, // testdata is not a test dir (no assertions run)
		"contest.go":             false, // must not match on substring
		"":                       false,
	}
	for in, want := range tests {
		if got := looksLikeTestFile(in); got != want {
			t.Errorf("looksLikeTestFile(%q) = %v, want %v", in, got, want)
		}
	}
}
