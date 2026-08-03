package main

import "testing"

// A typo'd -mode must fail loudly. The planner treats anything that is not
// "session" as file_loop, so `-mode sesion` would silently run the default loop
// and report success — the user gets a green tick for a run that ignored their
// flag entirely, with nothing in the output to say so.
func TestValidateMode(t *testing.T) {
	valid := []string{"", "file_loop", "session"}
	for _, m := range valid {
		if err := validateMode(m); err != nil {
			t.Errorf("validateMode(%q) = %v, want nil", m, err)
		}
	}

	invalid := []string{"sesion", "Session", "SESSION", "agent", "file-loop", "loop"}
	for _, m := range invalid {
		err := validateMode(m)
		if err == nil {
			t.Errorf("validateMode(%q) = nil; an unrecognised mode must not be silently downgraded to file_loop", m)
			continue
		}
		// The message has to name the alternatives, or the user is left guessing
		// which spelling the flag wanted.
		if !contains(err.Error(), "file_loop") || !contains(err.Error(), "session") {
			t.Errorf("validateMode(%q) error %q should list the valid modes", m, err)
		}
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}
