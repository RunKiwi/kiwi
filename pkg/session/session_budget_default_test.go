package session

import (
	"testing"
	"time"
)

// The session default must sit well clear of the retired single-file loop cap ($0.50).
//
// The two loops are configured separately precisely because their costs differ
// by an order of magnitude; if this default ever drifts down to that older
// territory, sessions start halting in round one again and the symptom looks
// like a hard task rather than a bad constant.
func TestDefaultSessionBudgetExceedsRetiredSingleFileCap(t *testing.T) {
	const fileLoopCap = 0.50

	got := Config{}.withDefaults().SessionBudgetUSD
	if got <= fileLoopCap {
		t.Fatalf("default session budget %.2f is at or below the retired single-file cap %.2f", got, fileLoopCap)
	}
	if got != defaultSessionBudgetUSD {
		t.Errorf("withDefaults gave %.2f, want the package default %.2f", got, defaultSessionBudgetUSD)
	}
}

// An explicitly configured budget must survive withDefaults untouched, or the
// -session-budget flag and KIWI_SESSION_BUDGET_USD would be silently ignored.
func TestExplicitSessionBudgetIsPreserved(t *testing.T) {
	got := Config{SessionBudgetUSD: 12.50}.withDefaults().SessionBudgetUSD
	if got != 12.50 {
		t.Errorf("explicit budget was overwritten: got %.2f, want 12.50", got)
	}
}

// A round is capped relative to the session, not by a flat 15 minutes.
//
// The fixed value was invisible while the Free wall-clock cap was ten minutes:
// the session deadline always fired first. Raising the cap past 15 minutes made
// it live in the worst way — one round could consume most of the budget, and
// the review that makes a second round worth having would never happen.
func TestRoundDeadlineScalesWithTheSession(t *testing.T) {
	// Free at 20 minutes: three rounds should fit.
	c := Config{SessionDeadline: 20 * time.Minute}.withDefaults()
	if c.RoundDeadline*3 > c.SessionDeadline {
		t.Errorf("round deadline %s does not leave room for three rounds in %s",
			c.RoundDeadline, c.SessionDeadline)
	}

	// A long BYOC session keeps the ceiling: a round should not stretch to half
	// an hour just because the session may run for ninety minutes.
	long := Config{SessionDeadline: 90 * time.Minute}.withDefaults()
	if long.RoundDeadline != defaultRoundDeadline {
		t.Errorf("round deadline = %s, want the %s ceiling for a long session",
			long.RoundDeadline, defaultRoundDeadline)
	}
}

// An explicit value from the caller is never second-guessed.
func TestExplicitRoundDeadlineIsRespected(t *testing.T) {
	c := Config{SessionDeadline: 20 * time.Minute, RoundDeadline: 90 * time.Second}.withDefaults()
	if c.RoundDeadline != 90*time.Second {
		t.Errorf("round deadline = %s, want the caller's 90s", c.RoundDeadline)
	}
}
