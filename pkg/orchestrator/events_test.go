package orchestrator

import "testing"

func TestSummarize(t *testing.T) {
	if got := summarize("short", 100); got != "short" {
		t.Errorf("short unchanged: got %q", got)
	}
	long := ""
	for i := 0; i < 50; i++ {
		long += "0123456789"
	}
	got := summarize(long, 20)
	if len(got) > 20 {
		t.Errorf("truncated length %d > 20", len(got))
	}
	if got != long[len(long)-20:] {
		t.Errorf("summarize should keep the last 20 chars, got %q", got)
	}
}
