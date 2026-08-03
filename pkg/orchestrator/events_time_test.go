package orchestrator

import (
	"testing"
	"time"
)

// A progress flush carries several seconds of events at once. Stamping them on
// arrival gave every event in a batch the same instant, which is precisely the
// resolution needed to see where a run spends time it does not account for — so
// the daemon stamps each event as it happens and the Control Plane keeps it.
func TestEventTimePrefersTheDaemonStamp(t *testing.T) {
	stamped := time.Date(2026, 8, 3, 10, 30, 0, 0, time.UTC)
	if got := eventTime(stamped); !got.Equal(stamped) {
		t.Errorf("eventTime(%v) = %v, want the daemon's own stamp", stamped, got)
	}
}

// A daemon built before the field exists sends a zero time. Falling back to now
// reproduces the previous behaviour; using the zero value would sort every one
// of its events before every real one.
func TestEventTimeFallsBackForOlderDaemons(t *testing.T) {
	before := time.Now().Add(-time.Second)
	got := eventTime(time.Time{})
	if got.Before(before) {
		t.Errorf("a zero stamp should fall back to now, got %v", got)
	}
}

// Detail and arguments are truncated from opposite ends, on purpose: command
// output explains itself at the end, a tool call at its start.
func TestHeadAndSummarizeTruncateOppositeEnds(t *testing.T) {
	s := "START" + string(make([]byte, 100)) + "END"

	if got := headOf(s, 5); got != "START" {
		t.Errorf("headOf kept the wrong end: %q", got)
	}
	if got := summarize(s, 3); got != "END" {
		t.Errorf("summarize kept the wrong end: %q", got)
	}
	if got := headOf("short", 100); got != "short" {
		t.Errorf("headOf should pass short values through, got %q", got)
	}
}
