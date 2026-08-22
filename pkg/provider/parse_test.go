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

func TestCacheDiscountUSD(t *testing.T) {
	// For claude-opus-4-8: InputCostPerM = 5.00, readRate = 0.50 (10%).
	// Savings per 1M tokens = 5.00 - 0.50 = 4.50 USD.
	// For 1,000,000 cached tokens: 4.50 USD.
	if got := CacheDiscountUSD("claude-opus-4-8", 1_000_000); math.Abs(got-4.50) > 1e-9 {
		t.Errorf("CacheDiscountUSD(claude-opus-4-8, 1M)=%v want 4.50", got)
	}

	// For claude-sonnet-5: InputCostPerM = 3.00, readRate = 0.30 (10%).
	// Savings per 1M tokens = 3.00 - 0.30 = 2.70 USD.
	if got := CacheDiscountUSD("claude-sonnet-5", 1_000_000); math.Abs(got-2.70) > 1e-9 {
		t.Errorf("CacheDiscountUSD(claude-sonnet-5, 1M)=%v want 2.70", got)
	}

	// Zero or negative tokens return 0
	if got := CacheDiscountUSD("claude-opus-4-8", 0); got != 0 {
		t.Errorf("CacheDiscountUSD(0)=%v want 0", got)
	}
	if got := CacheDiscountUSD("claude-opus-4-8", -500); got != 0 {
		t.Errorf("CacheDiscountUSD(-500)=%v want 0", got)
	}
}
