package daemon

import (
	"strings"
	"testing"
)

// The real failure, from RunKiwi/website: src/app/layout.tsx imports
// next/font/google, and Next downloads the font on every build — cache or no
// cache. Phase A cannot help, because the fetch is part of the build, and the
// build is the thing that must run without network.
//
// Nothing here is fixable by the Actor, so the message has to say what
// happened. The alternative is what shipped before: six steps editing a
// component in response to a font download failure.
func TestNetworkRequired_NextFontIsNamed(t *testing.T) {
	out := `Failed to compile.
./src/app/layout.tsx
[next]/internal/font/google/outfit_ca35ecd4.module.css
Error: getaddrinfo EAI_AGAIN fonts.googleapis.com`

	why := networkRequired(out)
	if why == "" {
		t.Fatal("expected an offline-verification explanation")
	}
	if !strings.Contains(why, "Google Fonts") {
		t.Errorf("the cause should be named, got %q", why)
	}
	if !strings.Contains(why, "offline") && !strings.Contains(why, "network access is disabled") {
		t.Errorf("the message should explain the constraint, got %q", why)
	}
}

func TestNetworkRequired_Phrasings(t *testing.T) {
	for _, out := range []string{
		"dial tcp: lookup proxy.golang.org: no such host",
		"go: module fetch failed: network is unreachable",
		"curl: (6) Could not resolve host: registry.npmjs.org",
		"Error: getaddrinfo ENOTFOUND registry.yarnpkg.com",
		"pip: Temporary failure in name resolution",
	} {
		if networkRequired(out) == "" {
			t.Errorf("missed a network failure in %q", out)
		}
	}
}

// The expensive mistake is the other direction: reporting "needs network" for a
// task the Actor could actually have fixed means the user is told to change
// their repo when the agent simply gave up.
func TestNetworkRequired_OrdinaryFailuresAreNotBlamedOnTheNetwork(t *testing.T) {
	for _, out := range []string{
		"--- FAIL: TestDivide (0.00s)\n    math_test.go:12: got 0, want 3",
		"Tests: 1 failed, 4 passed",
		"TypeError: Cannot read properties of undefined (reading 'map')",
		"./src/app/layout.tsx:3:1: 'Footer' is defined but never used",
		"sh: npm: not found",
		"",
	} {
		if why := networkRequired(out); why != "" {
			t.Errorf("output %q was blamed on the network: %s", out, why)
		}
	}
}

// A network failure and a missing runtime are different problems with different
// remedies — one is unfixable here, the other is repaired by swapping the image
// — so they must not collapse into each other.
func TestNetworkRequired_IsDistinctFromAMissingRuntime(t *testing.T) {
	if f := classifyEnvOutput("Error: getaddrinfo EAI_AGAIN fonts.googleapis.com"); f != nil {
		t.Errorf("a network failure must not be treated as a repairable image fault: %+v", f)
	}
	if networkRequired("sh: npm: not found") != "" {
		t.Error("a missing runtime must not be reported as a network problem")
	}
}
