package daemon

import "testing"

// The session spend cap comes from SessionBudgetUSD, and passes through
// untouched.
//
// This started as a regression test for a shipped bug: there were two budget
// fields, the single-file loop's default was $0.50 against a session's $2-4
// design point, and the session runner read the wrong one. Nothing failed
// loudly — every session simply stopped at the budget rail around the end of
// round one and reported a halt, which reads like a hard task rather than a
// misconfiguration.
//
// The single-file loop is gone and so is its field, so that particular mix-up
// is now impossible to write. What is still worth pinning is the pass-through:
// the daemon must not substitute a number of its own.
func TestSessionLimits_PassesTheConfiguredBudgetThrough(t *testing.T) {
	d := &Daemon{config: Config{
		SessionBudgetUSD: 5.00,
		MaxRounds:        4,
	}}

	rounds, budget := d.sessionLimits()

	if budget != 5.00 {
		t.Errorf("session budget = %v, want 5.00", budget)
	}
	if rounds != 4 {
		t.Errorf("rounds = %d, want 4", rounds)
	}
}

// A daemon told nothing about session budgets must fall through to the session
// package's own default, not to zero-as-a-cap and not to a number invented
// here. The free-tier provisioner launches per-org daemons with a fixed argv,
// so this unset case is the one the fleet actually runs.
func TestSessionLimits_ZeroDefersToSessionPackageDefault(t *testing.T) {
	d := &Daemon{config: Config{}}

	rounds, budget := d.sessionLimits()

	// Zero, not a substitute: session.Config owns the default.
	if budget != 0 {
		t.Errorf("expected an unset budget to pass through as 0, got %v", budget)
	}
	if rounds != 0 {
		t.Errorf("expected unset rounds to pass through as 0, got %d", rounds)
	}
}
