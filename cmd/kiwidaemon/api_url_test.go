package main

import (
	"os/exec"
	"strings"
	"testing"
)

// The daemon's compiled-in -api-url default is what a BYOC operator gets when
// they run kiwidaemon without the flag. It pointed at api.runkiwi.com, a domain
// that does not resolve at all — so the out-of-the-box experience was a daemon
// that registered with nothing and reported a DNS error.
//
// This is a source check rather than a network call: a test must not depend on
// DNS, and the failure being guarded against is a typo'd constant, not an
// outage. Grepping the tree keeps every copy of the URL honest, including the
// ones in opsctl and the docs that drifted the same way.
func TestNoReferencesToTheDeadDomain(t *testing.T) {
	// Exclude this file: it names the dead domain in its own comments and in the
	// grep pattern, so an unfiltered search always matches itself and the test
	// can never pass. CI runs only ./pkg/..., which is why that went unnoticed.
	out, err := exec.Command("git", "grep", "-n", "api.runkiwi.com", "--", "..", ":!*api_url_test.go").CombinedOutput()
	if err != nil && len(out) == 0 {
		// git grep exits non-zero with no output when there are no matches,
		// which is the passing case.
		return
	}
	if len(strings.TrimSpace(string(out))) > 0 {
		t.Errorf("api.runkiwi.com does not resolve; use api.runkiwi.dev:\n%s", out)
	}
}
