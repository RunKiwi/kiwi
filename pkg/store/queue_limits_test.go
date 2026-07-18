package store

import (
	"context"
	"testing"
	"time"
)

func TestLeaseEnforcesConcurrencyCap(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// Set OrgLimits to MaxConcurrentJobs = 2
	err := s.db.Create(&OrgLimits{
		OrgID:             "o1",
		MaxConcurrentJobs: 2,
		MaxBudgetPerJob:   5.00,
	}).Error
	if err != nil {
		t.Fatalf("Create OrgLimits: %v", err)
	}

	// Enqueue 3 tasks
	enqueueTask(t, s, "t1", "o1", "j1")
	enqueueTask(t, s, "t2", "o1", "j2")
	enqueueTask(t, s, "t3", "o1", "j3")

	// 1st lease should succeed
	l1, err := s.LeaseNextTask(ctx, "o1", "d1", time.Minute)
	if err != nil || l1 == nil {
		t.Fatalf("Lease 1 failed: %v, %v", err, l1)
	}

	// 2nd lease should succeed
	l2, err := s.LeaseNextTask(ctx, "o1", "d1", time.Minute)
	if err != nil || l2 == nil {
		t.Fatalf("Lease 2 failed: %v, %v", err, l2)
	}

	// 3rd lease should return nil because of concurrency cap
	l3, err := s.LeaseNextTask(ctx, "o1", "d1", time.Minute)
	if err != nil {
		t.Fatalf("Lease 3 error: %v", err)
	}
	if l3 != nil {
		t.Fatalf("Expected nil for 3rd lease due to concurrency cap, got task %s", l3.ID)
	}
}

func TestLeaseEnforcesBudgetCap(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// Set OrgLimits to MaxBudgetPerJob = 5.00
	err := s.db.Create(&OrgLimits{
		OrgID:             "o1",
		MaxConcurrentJobs: 10,
		MaxBudgetPerJob:   5.00,
	}).Error
	if err != nil {
		t.Fatalf("Create OrgLimits: %v", err)
	}

	// Create Jobs
	s.db.Create(&Job{
		ID:      "j-good",
		OrgID:   "o1",
		UserID:  "u1",
		Status:  "RUNNING",
		CostUSD: 4.00,
	})
	s.db.Create(&Job{
		ID:      "j-bad",
		OrgID:   "o1",
		UserID:  "u1",
		Status:  "RUNNING",
		CostUSD: 6.00, // over budget
	})

	// Enqueue tasks for both jobs
	enqueueTask(t, s, "t-bad", "o1", "j-bad")
	enqueueTask(t, s, "t-good", "o1", "j-good")

	// First lease should skip t-bad (fail it) and return t-good
	l1, err := s.LeaseNextTask(ctx, "o1", "d1", time.Minute)
	if err != nil || l1 == nil {
		t.Fatalf("Lease failed: %v, %v", err, l1)
	}
	if l1.ID != "t-good" {
		t.Fatalf("Expected t-good, got %s", l1.ID)
	}

	// The bad task should be marked FAILED
	var badTask QueuedTask
	if err := s.db.First(&badTask, "id = ?", "t-bad").Error; err != nil {
		t.Fatalf("Find bad task: %v", err)
	}
	if badTask.Status != TaskFailed {
		t.Errorf("Expected t-bad to be FAILED, got %s", badTask.Status)
	}

	// The bad job should be marked FAILED
	var badJob Job
	if err := s.db.First(&badJob, "id = ?", "j-bad").Error; err != nil {
		t.Fatalf("Find bad job: %v", err)
	}
	if badJob.Status != "FAILED" {
		t.Errorf("Expected j-bad to be FAILED, got %s", badJob.Status)
	}
}
