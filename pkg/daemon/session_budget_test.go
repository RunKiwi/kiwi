package daemon

import "testing"

// The session spend cap must come from SessionBudgetUSD, never MaxBudgetUSD.
//
// This is a regression test for a shipped bug rather than a hypothetical. The
// two loops have different economics — file_loop is a few single-file calls and
// defaults to $0.50, a session is a task-long Architect driving an agentic
// Implementer and the design puts it at $2-4 — but the session runner read the
// file_loop field. Nothing failed loudly: every session simply stopped at the
// budget rail around the end of round one and reported a halt, which reads like
// a hard task rather than a misconfiguration.
func TestSessionLimits_UsesSessionBudgetNotFileLoopBudget(t *testing.T) {
	d := &Daemon{config: Config{
		MaxBudgetUSD:     0.50, // the file_loop default
		SessionBudgetUSD: 5.00,
		MaxRounds:        4,
	}}

	rounds, budget := d.sessionLimits()

	if budget == 0.50 {
		t.Fatal("session budget read MaxBudgetUSD (the file_loop cap); it must read SessionBudgetUSD")
	}
	if budget != 5.00 {
		t.Errorf("session budget = %v, want 5.00", budget)
	}
	if rounds != 4 {
		t.Errorf("rounds = %d, want 4", rounds)
	}
}

// A daemon told nothing about session budgets must fall through to the session
// package's own default, not to zero and not to the file_loop cap. The free-tier
// provisioner launches per-org daemons with a fixed argv, so this unset case is
// the one the fleet actually runs.
func TestSessionLimits_ZeroDefersToSessionPackageDefault(t *testing.T) {
	d := &Daemon{config: Config{MaxBudgetUSD: 0.50}}

	_, budget := d.sessionLimits()

	// Zero, not 0.50: the daemon must not substitute the file_loop cap, and must
	// not invent a number of its own. session.Config owns the default — see
	// TestDefaultSessionBudgetExceedsFileLoopCap in pkg/session for the value.
	if budget == 0.50 {
		t.Fatal("an unset session budget fell back to the file_loop cap")
	}
	if budget != 0 {
		t.Errorf("expected an unset budget to pass through as 0, got %v", budget)
	}
}
