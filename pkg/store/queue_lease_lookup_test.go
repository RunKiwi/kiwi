package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

func seedLeasedTask(t *testing.T, s *PostgresStore, id, orgID, leaseID string, expires time.Time, status string) {
	t.Helper()
	task := &QueuedTask{
		ID:             id,
		OrgID:          orgID,
		Status:         status,
		Spec:           map[string]any{"repo_url": "https://github.com/RunKiwi/kiwi"},
		LeaseID:        &leaseID,
		LeaseExpiresAt: &expires,
	}
	if err := s.db.Create(task).Error; err != nil {
		t.Fatalf("seed task: %v", err)
	}
}

func TestFindLeasedTaskReturnsTaskToLeaseHolder(t *testing.T) {
	s := newTestStore(t)
	seedLeasedTask(t, s, "task_1", "org_a", "lease_good", time.Now().UTC().Add(time.Minute), "LEASED")

	got, err := s.FindLeasedTask(context.Background(), "task_1", "lease_good")
	if err != nil {
		t.Fatalf("FindLeasedTask: %v", err)
	}
	if got.OrgID != "org_a" {
		t.Errorf("org = %q, want org_a", got.OrgID)
	}
	if got.Spec["repo_url"] != "https://github.com/RunKiwi/kiwi" {
		t.Errorf("spec repo_url = %v, want the seeded repo", got.Spec["repo_url"])
	}
}

// The fencing token is the whole authorisation basis for minting a credential.
// A daemon presenting the wrong one gets nothing, even with a real task id.
func TestFindLeasedTaskRejectsWrongLeaseID(t *testing.T) {
	s := newTestStore(t)
	seedLeasedTask(t, s, "task_1", "org_a", "lease_good", time.Now().UTC().Add(time.Minute), "LEASED")

	if _, err := s.FindLeasedTask(context.Background(), "task_1", "lease_stolen"); !errors.Is(err, ErrLeaseNotHeld) {
		t.Fatalf("err = %v, want ErrLeaseNotHeld", err)
	}
}

// Between a lease lapsing and the sweep reassigning it, the row still reads
// LEASED with the old holder's token. Status alone is not enough.
func TestFindLeasedTaskRejectsExpiredLease(t *testing.T) {
	s := newTestStore(t)
	seedLeasedTask(t, s, "task_1", "org_a", "lease_good", time.Now().UTC().Add(-time.Minute), "LEASED")

	if _, err := s.FindLeasedTask(context.Background(), "task_1", "lease_good"); !errors.Is(err, ErrLeaseNotHeld) {
		t.Fatalf("err = %v, want ErrLeaseNotHeld for a lapsed lease", err)
	}
}

func TestFindLeasedTaskRejectsFinishedTask(t *testing.T) {
	s := newTestStore(t)
	seedLeasedTask(t, s, "task_1", "org_a", "lease_good", time.Now().UTC().Add(time.Minute), "SUCCEEDED")

	if _, err := s.FindLeasedTask(context.Background(), "task_1", "lease_good"); !errors.Is(err, ErrLeaseNotHeld) {
		t.Fatalf("err = %v, want ErrLeaseNotHeld for a finished task", err)
	}
}

func TestFindLeasedTaskRejectsEmptyInput(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	for _, tc := range []struct{ task, lease string }{{"", "l"}, {"t", ""}, {"", ""}} {
		if _, err := s.FindLeasedTask(ctx, tc.task, tc.lease); !errors.Is(err, ErrLeaseNotHeld) {
			t.Errorf("FindLeasedTask(%q,%q) = %v, want ErrLeaseNotHeld", tc.task, tc.lease, err)
		}
	}
}
