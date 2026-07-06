package provider

import (
	"math"
	"testing"
)

func TestExtractCode(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"plain fence", "here:\n```\nhello\n```\ndone", "hello"},
		{"lang-tagged fence", "```go\npackage x\n```", "package x"},
		{"no fence", "  just text  ", "just text"},
		{"first of two", "```\nA\n```\nmid\n```\nB\n```", "A"},
	}
	for _, c := range cases {
		if got := extractCode(c.in); got != c.want {
			t.Errorf("%s: extractCode()=%q want %q", c.name, got, c.want)
		}
	}
}

func TestParseVerdict(t *testing.T) {
	if v := parseVerdict(`{"approved": true, "reasons": "looks good"}`); !v.Approved || v.Reasons != "looks good" {
		t.Errorf("clean json: got %+v", v)
	}
	if v := parseVerdict("Sure!\n{\"approved\": false, \"reasons\": \"unsafe\"}\nthanks"); v.Approved || v.Reasons != "unsafe" {
		t.Errorf("embedded json: got %+v", v)
	}
	if v := parseVerdict("no json here"); v.Approved {
		t.Errorf("malformed must reject, got %+v", v)
	}
}

func TestCostUSD(t *testing.T) {
	// 1M input + 1M output = 5 + 25 = 30
	if got := costUSD(1_000_000, 1_000_000); math.Abs(got-30.0) > 1e-9 {
		t.Errorf("costUSD=%v want 30", got)
	}
}
