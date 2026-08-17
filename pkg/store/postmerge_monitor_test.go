package store

import (
	"context"
	"testing"
	"time"
)

func TestPostMergeMonitorRoundTrip(t *testing.T) {
	s := newTestStore(t)
	m := &PostMergeMonitor{
		ID:             "mon_1",
		OrgID:          "org1",
		JobID:          "job1",
		Repo:           "acme/widgets",
		PRNumber:       42,
		MergeCommitSHA: "abc123",
		Status:         MonitorStatusMonitoring,
		DeployedAt:     mustParseTime(t, "2026-08-15T00:00:00Z"),
		WindowEndsAt:   mustParseTime(t, "2026-08-16T00:00:00Z"),
	}
	if err := s.DB().Create(m).Error; err != nil {
		t.Fatalf("create: %v", err)
	}

	var got PostMergeMonitor
	if err := s.DB().First(&got, "id = ?", "mon_1").Error; err != nil {
		t.Fatalf("read back: %v", err)
	}
	if got.Status != MonitorStatusMonitoring {
		t.Errorf("status = %q, want %q", got.Status, MonitorStatusMonitoring)
	}
	if got.RemediationTaskID != nil {
		t.Errorf("remediation_task_id = %v, want nil on a fresh monitor", got.RemediationTaskID)
	}
}

func mustParseTime(t *testing.T, s string) time.Time {
	t.Helper()
	tm, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatalf("parse time %q: %v", s, err)
	}
	return tm
}

func TestCreateAndLookUpMonitorByMergeCommit(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	m := &PostMergeMonitor{
		ID: "mon_1", OrgID: "org1", JobID: "job1", Repo: "acme/widgets", PRNumber: 42,
		MergeCommitSHA: "abc123", Status: MonitorStatusMonitoring,
		DeployedAt:   mustParseTime(t, "2026-08-15T00:00:00Z"),
		WindowEndsAt: mustParseTime(t, "2026-08-16T00:00:00Z"),
	}
	if err := s.CreateMonitor(ctx, m); err != nil {
		t.Fatalf("create: %v", err)
	}

	got, err := s.GetMonitorByMergeCommit(ctx, "org1", "abc123")
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if got.ID != "mon_1" {
		t.Errorf("id = %q, want mon_1", got.ID)
	}
}

func TestGetMonitorByMergeCommitIgnoresFinalized(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	m := &PostMergeMonitor{
		ID: "mon_1", OrgID: "org1", JobID: "job1", Repo: "acme/widgets", PRNumber: 42,
		MergeCommitSHA: "abc123", Status: MonitorStatusVerified,
		DeployedAt:   mustParseTime(t, "2026-08-15T00:00:00Z"),
		WindowEndsAt: mustParseTime(t, "2026-08-16T00:00:00Z"),
	}
	if err := s.DB().Create(m).Error; err != nil {
		t.Fatal(err)
	}

	if _, err := s.GetMonitorByMergeCommit(ctx, "org1", "abc123"); err == nil {
		t.Errorf("expected not-found for an already-finalized monitor, got a match")
	}
}

func TestFinalizeMonitorIsSingleFire(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	m := &PostMergeMonitor{
		ID: "mon_1", OrgID: "org1", JobID: "job1", Repo: "acme/widgets", PRNumber: 42,
		MergeCommitSHA: "abc123", Status: MonitorStatusMonitoring,
		// Window must still be open — FinalizeMonitor requires window_ends_at
		// in the future for a REGRESSION verdict (see its doc comment), and
		// this test's first call is exactly that verdict.
		DeployedAt:   time.Now(),
		WindowEndsAt: time.Now().Add(24 * time.Hour),
	}
	if err := s.CreateMonitor(ctx, m); err != nil {
		t.Fatal(err)
	}

	won, err := s.FinalizeMonitor(ctx, "mon_1", MonitorStatusRegression, "revert PR #43")
	if err != nil {
		t.Fatal(err)
	}
	if !won {
		t.Fatalf("first FinalizeMonitor call should win the race")
	}

	wonAgain, err := s.FinalizeMonitor(ctx, "mon_1", MonitorStatusVerified, "window elapsed")
	if err != nil {
		t.Fatal(err)
	}
	if wonAgain {
		t.Errorf("second FinalizeMonitor call should not win — monitor is already finalized")
	}

	var got PostMergeMonitor
	if err := s.DB().First(&got, "id = ?", "mon_1").Error; err != nil {
		t.Fatal(err)
	}
	if got.Status != MonitorStatusRegression {
		t.Errorf("status = %q, want %q (the first, winning call)", got.Status, MonitorStatusRegression)
	}
	if got.VerdictEvidence != "revert PR #43" {
		t.Errorf("evidence = %q, want the first call's evidence", got.VerdictEvidence)
	}
}

// TestFinalizeMonitorRejectsRegressionPastWindow covers the race between a
// late-arriving webhook and the periodic sweep: a monitor whose window has
// already elapsed is still status = MONITORING until the sweep runs, so a
// revert or failed-check-run event landing in that gap must not be able to
// write a REGRESSION verdict for a window that already closed clean.
func TestFinalizeMonitorRejectsRegressionPastWindow(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	m := &PostMergeMonitor{
		ID: "mon_1", OrgID: "org1", JobID: "job1", Repo: "acme/widgets", PRNumber: 42,
		MergeCommitSHA: "abc123", Status: MonitorStatusMonitoring,
		DeployedAt:   time.Now().Add(-25 * time.Hour),
		WindowEndsAt: time.Now().Add(-1 * time.Hour), // already elapsed
	}
	if err := s.CreateMonitor(ctx, m); err != nil {
		t.Fatal(err)
	}

	won, err := s.FinalizeMonitor(ctx, "mon_1", MonitorStatusRegression, "check run failed on abc")
	if err != nil {
		t.Fatal(err)
	}
	if won {
		t.Errorf("FinalizeMonitor(REGRESSION) should not win once the window has elapsed")
	}

	var got PostMergeMonitor
	if err := s.DB().First(&got, "id = ?", "mon_1").Error; err != nil {
		t.Fatal(err)
	}
	if got.Status != MonitorStatusMonitoring {
		t.Errorf("status = %q, want unchanged MONITORING — the sweep, not a late signal, should finalize this one", got.Status)
	}

	// VERIFIED carries no such restriction — the sweep must still be able to
	// finalize the same past-window monitor.
	wonVerified, err := s.FinalizeMonitor(ctx, "mon_1", MonitorStatusVerified, "24h window elapsed with no regression signal")
	if err != nil {
		t.Fatal(err)
	}
	if !wonVerified {
		t.Errorf("FinalizeMonitor(VERIFIED) should still win past the window — that's exactly what the sweep does")
	}
}

func TestListMonitorsPastWindow(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	past := &PostMergeMonitor{
		ID: "mon_past", OrgID: "org1", JobID: "job1", Repo: "acme/widgets", PRNumber: 1,
		MergeCommitSHA: "sha1", Status: MonitorStatusMonitoring,
		DeployedAt:   mustParseTime(t, "2026-08-14T00:00:00Z"),
		WindowEndsAt: mustParseTime(t, "2026-08-15T00:00:00Z"),
	}
	future := &PostMergeMonitor{
		ID: "mon_future", OrgID: "org1", JobID: "job2", Repo: "acme/widgets", PRNumber: 2,
		MergeCommitSHA: "sha2", Status: MonitorStatusMonitoring,
		DeployedAt:   mustParseTime(t, "2026-08-15T00:00:00Z"),
		WindowEndsAt: mustParseTime(t, "2026-08-20T00:00:00Z"),
	}
	if err := s.CreateMonitor(ctx, past); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateMonitor(ctx, future); err != nil {
		t.Fatal(err)
	}

	got, err := s.ListMonitorsPastWindow(ctx, mustParseTime(t, "2026-08-15T00:00:01Z"))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != "mon_past" {
		t.Errorf("got %+v, want exactly [mon_past]", got)
	}
}

// TestSetMonitorRemediationTaskID is not in the task brief's test list but
// closes a coverage gap the brief's own implementation snippet introduces:
// SetMonitorRemediationTaskID writes a plain string into a *string column
// via a raw column Update, and nothing else in this suite proves the value
// actually lands and reads back non-nil.
func TestSetMonitorRemediationTaskID(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	m := &PostMergeMonitor{
		ID: "mon_1", OrgID: "org1", JobID: "job1", Repo: "acme/widgets", PRNumber: 42,
		MergeCommitSHA: "abc123", Status: MonitorStatusMonitoring,
		DeployedAt:   mustParseTime(t, "2026-08-15T00:00:00Z"),
		WindowEndsAt: mustParseTime(t, "2026-08-16T00:00:00Z"),
	}
	if err := s.CreateMonitor(ctx, m); err != nil {
		t.Fatal(err)
	}

	if err := s.SetMonitorRemediationTaskID(ctx, "mon_1", "task_1"); err != nil {
		t.Fatalf("SetMonitorRemediationTaskID: %v", err)
	}

	var got PostMergeMonitor
	if err := s.DB().First(&got, "id = ?", "mon_1").Error; err != nil {
		t.Fatal(err)
	}
	if got.RemediationTaskID == nil || *got.RemediationTaskID != "task_1" {
		t.Errorf("remediation_task_id = %v, want \"task_1\"", got.RemediationTaskID)
	}
}
