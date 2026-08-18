package store

import (
	"context"
	"testing"
	"time"
)

func newTestPoll(id, orgID string, nextPollAt time.Time) *PostMergeTelemetryPoll {
	return &PostMergeTelemetryPoll{
		ID:            id,
		OrgID:         orgID,
		MonitorID:     "mon_1",
		Provider:      "datadog",
		Query:         "p95:trace.checkout{env:prod}",
		BaselineStart: time.Now().Add(-24 * time.Hour),
		BaselineEnd:   time.Now().Add(-23 * time.Hour),
		CurrentStart:  time.Now().Add(-15 * time.Minute),
		CurrentEnd:    time.Now(),
		NextPollAt:    nextPollAt,
		WindowEndsAt:  time.Now().Add(4 * time.Hour),
	}
}

func TestClaimDuePollsOnlyClaimsDueUnclaimedRows(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := time.Now()

	due := newTestPoll("poll_due", "org1", now.Add(-1*time.Minute))
	notDue := newTestPoll("poll_not_due", "org1", now.Add(10*time.Minute))
	otherOrg := newTestPoll("poll_other_org", "org2", now.Add(-1*time.Minute))

	for _, p := range []*PostMergeTelemetryPoll{due, notDue, otherOrg} {
		if err := s.CreateTelemetryPoll(ctx, p); err != nil {
			t.Fatalf("create %s: %v", p.ID, err)
		}
	}

	claimed, err := s.ClaimDuePolls(ctx, "org1", now, 10)
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if len(claimed) != 1 || claimed[0].ID != "poll_due" {
		t.Fatalf("claimed = %+v, want exactly poll_due", claimed)
	}

	// A second claim attempt must not re-claim the same row — it's already
	// claimed, so a concurrent daemon-facing due-check must not double-send it.
	claimedAgain, err := s.ClaimDuePolls(ctx, "org1", now, 10)
	if err != nil {
		t.Fatalf("second claim: %v", err)
	}
	if len(claimedAgain) != 0 {
		t.Errorf("second claim returned %d rows, want 0 (already claimed)", len(claimedAgain))
	}
}

func TestRecordPollResultReschedulesAndClearsClaim(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := time.Now()

	p := newTestPoll("poll_1", "org1", now.Add(-1*time.Minute))
	if err := s.CreateTelemetryPoll(ctx, p); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := s.ClaimDuePolls(ctx, "org1", now, 10); err != nil {
		t.Fatalf("claim: %v", err)
	}

	next := now.Add(15 * time.Minute)
	if err := s.RecordPollResult(ctx, "poll_1", next, `{"baseline":{"sample_count":40,"mean":100},"current":{"sample_count":40,"mean":105}}`, true, now); err != nil {
		t.Fatalf("record result: %v", err)
	}

	var got PostMergeTelemetryPoll
	if err := s.DB().First(&got, "id = ?", "poll_1").Error; err != nil {
		t.Fatalf("read back: %v", err)
	}
	if got.ClaimedAt != nil {
		t.Errorf("claimed_at = %v, want cleared after report", got.ClaimedAt)
	}
	if !got.NextPollAt.Equal(next) {
		t.Errorf("next_poll_at = %v, want %v", got.NextPollAt, next)
	}
	if got.LastResult == "" {
		t.Errorf("last_result not persisted")
	}
}

func TestRecordPollResultWithoutRescheduleLeavesItUnclaimed(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := time.Now()

	p := newTestPoll("poll_1", "org1", now.Add(-1*time.Minute))
	if err := s.CreateTelemetryPoll(ctx, p); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := s.ClaimDuePolls(ctx, "org1", now, 10); err != nil {
		t.Fatalf("claim: %v", err)
	}

	// reschedule=false is what a REGRESSION verdict (Task 11) uses — the
	// monitor is finalizing, so there is no next poll to schedule.
	if err := s.RecordPollResult(ctx, "poll_1", now, `{}`, false, now); err != nil {
		t.Fatalf("record result: %v", err)
	}

	claimed, err := s.ClaimDuePolls(ctx, "org1", now.Add(time.Hour), 10)
	if err != nil {
		t.Fatalf("claim after no-reschedule: %v", err)
	}
	if len(claimed) != 0 {
		t.Errorf("claimed %d rows an hour later, want 0 — a non-rescheduled poll must never become due again", len(claimed))
	}
}

func TestRecordPollResultAdvancesTheComparisonWindow(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := time.Now()

	p := newTestPoll("poll_1", "org1", now.Add(-1*time.Minute))
	mergedAt := now.Add(-30 * time.Minute)
	p.CurrentStart = mergedAt
	p.CurrentEnd = mergedAt.Add(time.Second) // as created: ~1 second wide
	if err := s.CreateTelemetryPoll(ctx, p); err != nil {
		t.Fatalf("create: %v", err)
	}

	// The scheduling clock (next, ~15 minutes out) and the window clock
	// (currentEnd, now) are deliberately different values here — passing the
	// former as the latter would ask the daemon to query a future range.
	next := now.Add(15 * time.Minute)
	if err := s.RecordPollResult(ctx, "poll_1", next, `{}`, true, now); err != nil {
		t.Fatalf("record result: %v", err)
	}

	var got PostMergeTelemetryPoll
	if err := s.DB().First(&got, "id = ?", "poll_1").Error; err != nil {
		t.Fatalf("read back: %v", err)
	}
	if !got.CurrentEnd.After(p.CurrentEnd) {
		t.Errorf("current_end = %v, want advanced past %v", got.CurrentEnd, p.CurrentEnd)
	}
	if got.CurrentEnd.After(next.Add(-time.Minute)) {
		t.Errorf("current_end = %v, want ~now, not the future next_poll_at %v", got.CurrentEnd, next)
	}
	if drift := got.CurrentStart.Sub(mergedAt); drift > time.Second || drift < -time.Second {
		t.Errorf("current_start = %v, want unchanged at %v", got.CurrentStart, mergedAt)
	}
}

// TestRetirePastWindowPollsRetiresNeverClaimedPollsIdempotently covers the
// leak where an org's daemon scaled to zero and never came back before a
// poll's 4h window closed: expiry is otherwise only checked on a report, so
// a never-claimed poll matched the due filter forever and would be claimed
// in bulk whenever the daemon eventually cold-started. The second sweep
// asserting zero is what actually covers the next_poll_at <= window_ends_at
// clause — without it this would pass as a plain unconditional UPDATE.
func TestRetirePastWindowPollsRetiresNeverClaimedPollsIdempotently(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := time.Now()

	expired := newTestPoll("poll_expired", "org1", now.Add(-3*time.Hour))
	expired.WindowEndsAt = now.Add(-1 * time.Hour) // window closed, never claimed
	live := newTestPoll("poll_live", "org1", now.Add(-1*time.Minute))
	live.WindowEndsAt = now.Add(2 * time.Hour) // still inside its window
	for _, p := range []*PostMergeTelemetryPoll{expired, live} {
		if err := s.CreateTelemetryPoll(ctx, p); err != nil {
			t.Fatalf("create %s: %v", p.ID, err)
		}
	}

	retired, err := s.RetirePastWindowPolls(ctx, now)
	if err != nil {
		t.Fatalf("retire: %v", err)
	}
	if retired != 1 {
		t.Fatalf("retired = %d, want 1", retired)
	}

	// A second sweep must be a no-op — an already-retired row's next_poll_at
	// is far past its window_ends_at, so it no longer matches.
	again, err := s.RetirePastWindowPolls(ctx, time.Now())
	if err != nil {
		t.Fatalf("second retire: %v", err)
	}
	if again != 0 {
		t.Errorf("second sweep retired = %d, want 0 — the sweep must be idempotent", again)
	}

	claimed, err := s.ClaimDuePolls(ctx, "org1", now, 10)
	if err != nil {
		t.Fatalf("claim after retire: %v", err)
	}
	if len(claimed) != 1 || claimed[0].ID != "poll_live" {
		t.Errorf("claimed = %+v, want only poll_live — a retired poll must never become due again", claimed)
	}
}

func TestRetirePastWindowPollsLeavesClaimedPollsAlone(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := time.Now()

	// Claimed but past its window: the daemon is (or was) working on it, so
	// it drains through the report path or the stale-claim sweep, not here.
	p := newTestPoll("poll_1", "org1", now.Add(-3*time.Hour))
	p.WindowEndsAt = now.Add(-1 * time.Hour)
	if err := s.CreateTelemetryPoll(ctx, p); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := s.ClaimDuePolls(ctx, "org1", now, 10); err != nil {
		t.Fatalf("claim: %v", err)
	}

	retired, err := s.RetirePastWindowPolls(ctx, now)
	if err != nil {
		t.Fatalf("retire: %v", err)
	}
	if retired != 0 {
		t.Errorf("retired = %d, want 0 — a claimed poll is not this sweep's business", retired)
	}
}

func TestReleaseStalePollsUnsticksAnUnreportedClaim(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := time.Now()

	p := newTestPoll("poll_1", "org1", now.Add(-20*time.Minute))
	if err := s.CreateTelemetryPoll(ctx, p); err != nil {
		t.Fatalf("create: %v", err)
	}
	if _, err := s.ClaimDuePolls(ctx, "org1", now.Add(-20*time.Minute), 10); err != nil {
		t.Fatalf("claim: %v", err)
	}

	// The daemon never reported back. 10 minutes later the sweep should
	// release the stale claim so it can be retried.
	released, err := s.ReleaseStalePolls(ctx, now.Add(-10*time.Minute))
	if err != nil {
		t.Fatalf("release: %v", err)
	}
	if released != 1 {
		t.Fatalf("released = %d, want 1", released)
	}

	claimed, err := s.ClaimDuePolls(ctx, "org1", now, 10)
	if err != nil {
		t.Fatalf("re-claim after release: %v", err)
	}
	if len(claimed) != 1 {
		t.Errorf("re-claimed %d rows after release, want 1", len(claimed))
	}
}
