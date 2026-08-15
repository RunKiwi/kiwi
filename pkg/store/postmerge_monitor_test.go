package store

import (
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
