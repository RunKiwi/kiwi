package auth

import "testing"

// The Free per-task cap is the rail that was binding on session mode: $5 buys
// three or four rounds, and ten minutes cut runs off with budget still unspent.
//
// It is asserted here rather than left as a bare literal because raising it is
// not only a code change — every existing Free org carries the old value in its
// org_limits row, and the enforcing read goes straight to that table. A change
// to this constant without the matching migration reaches new signups only.
func TestFreeTaskTimeoutIsTwentyMinutes(t *testing.T) {
	if got := FreeLimits("org_1").TaskTimeoutSeconds; got != 1200 {
		t.Errorf("Free task timeout = %ds, want 1200 (see migrations/0023)", got)
	}
}

// Free stays below the platform default deliberately: wall clock is what the
// tier meters, so the cap and the agent-minute allowance are the same lever.
func TestFreeTaskTimeoutStaysUnderTheDefault(t *testing.T) {
	free := FreeLimits("org_1").TaskTimeoutSeconds
	def := DefaultLimits("org_1").TaskTimeoutSeconds
	if free >= def {
		t.Errorf("Free timeout (%ds) should stay below the default (%ds)", free, def)
	}
}

// At 500 agent-minutes a month, the cap decides how many maximum-length tasks a
// Free org can run. Stated as a test because it is a pricing fact, not an
// implementation detail: raising the cap silently divides the allowance.
func TestFreeMonthlyTaskBudgetIsAboutTwentyFive(t *testing.T) {
	l := FreeLimits("org_1")
	tasks := int(l.MaxAgentMinutesPerMonth) / (l.TaskTimeoutSeconds / 60)
	if tasks < 20 || tasks > 30 {
		t.Errorf("a Free org gets %d maximum-length tasks a month; "+
			"if that is intended, update this test and the pricing page", tasks)
	}
}
