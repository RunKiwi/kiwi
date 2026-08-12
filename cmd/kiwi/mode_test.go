package main

import (
	"strings"
	"testing"
)

// -mode used to choose between the single-file loop and the session loop. The
// single-file loop is gone, so the flag decides nothing — but a script that
// still passes it must keep working, and the user must be told the flag stopped
// meaning anything. Silently ignoring it is how someone concludes the tool is
// broken; erroring on it punishes them for our change.
func TestDeprecatedModeNotice(t *testing.T) {
	if got := deprecatedModeNotice(""); got != "" {
		t.Errorf("deprecatedModeNotice(\"\") = %q, want no notice", got)
	}

	for _, m := range []string{"session", "file_loop", "sesion"} {
		got := deprecatedModeNotice(m)
		if got == "" {
			t.Errorf("deprecatedModeNotice(%q) = \"\"; a caller passing the flag must be told it is ignored", m)
			continue
		}
		if !strings.Contains(got, m) {
			t.Errorf("deprecatedModeNotice(%q) = %q; the notice should quote what was passed", m, got)
		}
		if !strings.Contains(got, "ignored") {
			t.Errorf("deprecatedModeNotice(%q) = %q; the notice should say the flag is ignored", m, got)
		}
	}
}
