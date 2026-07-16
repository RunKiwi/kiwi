package store

import (
	"context"
	"testing"
	"time"
)

func enqueue(t *testing.T, s *PostgresStore, id, org string) {
	t.Helper()
	err := s.EnqueueTask(context.Background(), &QueuedTask{
		ID:    id,
		OrgID: org,
		JobID: "job-" + id,
		Spec:  map[string]interface{}{"task": "fix it", "id": id},
	})
	if err != nil {
		t.Fatalf("EnqueueTask(%s): %v", id, err)
	}
}

func TestEnqueueAndLease(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	enqueue(t, s, "t1", "o1")

	leased, err := s.LeaseNextTask(ctx, "o1", "daemon-1", time.Minute)
	if err != nil {
		t.Fatalf("LeaseNextTask: %v", err)
	}
	if leased == nil {
		t.Fatal("expected a leased task, got nil")
	}
	if leased.ID != "t1" || leased.Status != TaskLeased {
		t.Errorf("got id=%s status=%s, want t1/LEASED", leased.ID, leased.Status)
	}
	if leased.LeaseID == nil || *leased.LeaseID == "" {
		t.Error("expected a lease id (fencing token)")
	}
	if leased.LeasedBy == nil || *leased.LeasedBy != "daemon-1" {
		t.Errorf("expected leased_by daemon-1, got %v", leased.LeasedBy)
	}
	if leased.Attempts != 1 {
		t.Errorf("expected attempts=1, got %d", leased.Attempts)
	}

	// Nothing left QUEUED → next lease returns nil, not the same task.
	again, err := s.LeaseNextTask(ctx, "o1", "daemon-2", time.Minute)
	if err != nil {
		t.Fatalf("second LeaseNextTask: %v", err)
	}
	if again != nil {
		t.Errorf("expected nil (no queued work), got task %s", again.ID)
	}
}

func TestLeaseOrgScoping(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	enqueue(t, s, "a1", "orgA")
	enqueue(t, s, "b1", "orgB")

	leased, err := s.LeaseNextTask(ctx, "orgB", "d", time.Minute)
	if err != nil {
		t.Fatalf("LeaseNextTask: %v", err)
	}
	if leased == nil || leased.ID != "b1" {
		t.Fatalf("expected orgB's task b1, got %v", leased)
	}
}

func TestLeaseFIFOOrdering(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	enqueue(t, s, "first", "o1")
	// Ensure a distinct created_at ordering.
	time.Sleep(2 * time.Millisecond)
	enqueue(t, s, "second", "o1")

	leased, err := s.LeaseNextTask(ctx, "o1", "d", time.Minute)
	if err != nil {
		t.Fatalf("LeaseNextTask: %v", err)
	}
	if leased.ID != "first" {
		t.Errorf("expected oldest task 'first', got %s", leased.ID)
	}
}

func TestRenewLease(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	enqueue(t, s, "t1", "o1")
	leased, _ := s.LeaseNextTask(ctx, "o1", "d", time.Minute)
	origExpiry := *leased.LeaseExpiresAt

	// Wrong token cannot renew.
	ok, err := s.RenewLease(ctx, "t1", "wrong-lease", time.Hour)
	if err != nil {
		t.Fatalf("RenewLease(wrong): %v", err)
	}
	if ok {
		t.Error("renew with wrong lease id must fail")
	}

	// Correct token extends the expiry.
	ok, err = s.RenewLease(ctx, "t1", *leased.LeaseID, time.Hour)
	if err != nil {
		t.Fatalf("RenewLease: %v", err)
	}
	if !ok {
		t.Fatal("renew with correct lease id must succeed")
	}
	var got QueuedTask
	s.DB().First(&got, "id = ?", "t1")
	if !got.LeaseExpiresAt.After(origExpiry) {
		t.Errorf("expected extended expiry, orig=%v new=%v", origExpiry, got.LeaseExpiresAt)
	}
}

func TestCompleteTaskFencing(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	enqueue(t, s, "t1", "o1")
	leased, _ := s.LeaseNextTask(ctx, "o1", "d", time.Minute)

	// Stale/wrong token cannot complete.
	ok, err := s.CompleteTask(ctx, "t1", "stale-lease", TaskSucceeded)
	if err != nil {
		t.Fatalf("CompleteTask(stale): %v", err)
	}
	if ok {
		t.Error("completing with a stale lease id must be rejected (fencing)")
	}

	// Invalid final status is an error.
	if _, err := s.CompleteTask(ctx, "t1", *leased.LeaseID, "BOGUS"); err == nil {
		t.Error("expected error for invalid final status")
	}

	// Correct token completes.
	ok, err = s.CompleteTask(ctx, "t1", *leased.LeaseID, TaskSucceeded)
	if err != nil {
		t.Fatalf("CompleteTask: %v", err)
	}
	if !ok {
		t.Fatal("completing with the correct lease id must succeed")
	}
	var got QueuedTask
	s.DB().First(&got, "id = ?", "t1")
	if got.Status != TaskSucceeded {
		t.Errorf("status = %s, want SUCCEEDED", got.Status)
	}

	// Double-complete is a no-op.
	ok, _ = s.CompleteTask(ctx, "t1", *leased.LeaseID, TaskFailed)
	if ok {
		t.Error("completing an already-terminal task must be a no-op")
	}
}

func TestRequeueExpiredLeasesAndRefencing(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	enqueue(t, s, "t1", "o1")

	first, _ := s.LeaseNextTask(ctx, "o1", "daemon-1", time.Minute)
	oldLease := *first.LeaseID

	// Simulate daemon-1 dying: force the lease into the past.
	past := time.Now().Add(-time.Minute)
	if err := s.DB().Model(&QueuedTask{}).Where("id = ?", "t1").
		Update("lease_expires_at", past).Error; err != nil {
		t.Fatalf("force-expire: %v", err)
	}

	n, err := s.RequeueExpiredLeases(ctx)
	if err != nil {
		t.Fatalf("RequeueExpiredLeases: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected 1 task requeued, got %d", n)
	}

	var got QueuedTask
	s.DB().First(&got, "id = ?", "t1")
	if got.Status != TaskQueued || got.LeaseID != nil || got.LeasedBy != nil {
		t.Errorf("requeued task should be QUEUED with cleared lease, got %+v", got)
	}

	// A new daemon can now re-lease it with a fresh token.
	second, err := s.LeaseNextTask(ctx, "o1", "daemon-2", time.Minute)
	if err != nil || second == nil {
		t.Fatalf("re-lease after requeue failed: %v", err)
	}
	if *second.LeaseID == oldLease {
		t.Error("re-lease should mint a new fencing token")
	}
	if second.Attempts != 2 {
		t.Errorf("expected attempts=2 after re-lease, got %d", second.Attempts)
	}

	// The dead daemon-1 waking up cannot complete with its stale token.
	ok, _ := s.CompleteTask(ctx, "t1", oldLease, TaskSucceeded)
	if ok {
		t.Error("stale daemon must not complete a reassigned task (fencing after requeue)")
	}
}
