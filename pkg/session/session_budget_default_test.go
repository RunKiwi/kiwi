package session

import "testing"

// The session default must sit well clear of the file_loop cap ($0.50).
//
// The two loops are configured separately precisely because their costs differ
// by an order of magnitude; if this default ever drifts down to file_loop
// territory, sessions start halting in round one again and the symptom looks
// like a hard task rather than a bad constant.
func TestDefaultSessionBudgetExceedsFileLoopCap(t *testing.T) {
	const fileLoopCap = 0.50

	got := Config{}.withDefaults().SessionBudgetUSD
	if got <= fileLoopCap {
		t.Fatalf("default session budget %.2f is at or below the file_loop cap %.2f", got, fileLoopCap)
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
