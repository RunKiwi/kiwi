// pkg/session/investigation_test.go
package session

import "testing"

func TestParseSpecAcceptsNoDiffExpectedOnApprove(t *testing.T) {
	resp := `{"verdict": "approve", "summary": "Found the root cause: a nil check missing in auth.go.", "no_diff_expected": true}`
	s, err := parseSpec(resp)
	if err != nil {
		t.Fatalf("parseSpec: %v", err)
	}
	if !s.NoDiffExpected {
		t.Fatal("expected NoDiffExpected to round-trip as true")
	}
}

func TestParseSpecDefaultsNoDiffExpectedToFalse(t *testing.T) {
	resp := `{"verdict": "approve", "summary": "Fixed it."}`
	s, err := parseSpec(resp)
	if err != nil {
		t.Fatalf("parseSpec: %v", err)
	}
	if s.NoDiffExpected {
		t.Fatal("expected NoDiffExpected to default to false when the Architect doesn't say otherwise — a normal task must never accidentally skip the diff requirement")
	}
}
