package client

import "testing"

func TestLogDelta(t *testing.T) {
	if got := logDelta("", "hello"); got != "hello" {
		t.Errorf("empty prev: got %q", got)
	}
	if got := logDelta("hello", "hello world"); got != " world" {
		t.Errorf("growing: got %q", got)
	}
	if got := logDelta("hello", "hello"); got != "" {
		t.Errorf("no change: got %q", got)
	}
	if got := logDelta("abc", "xyz123"); got != "xyz123" {
		t.Errorf("rewritten (non-prefix): got %q", got)
	}
}
