package store

import (
	"context"
	"testing"
	"time"
)

// enqueueFleetTask enqueues a ready task pinned to a fleet (fleetID may be "").
func enqueueFleetTask(t *testing.T, s *PostgresStore, id, org, fleet string) {
	t.Helper()
	if err := s.EnqueueTask(context.Background(), &QueuedTask{
		ID:      id,
		OrgID:   org,
		JobID:   "job-" + id,
		FleetID: fleet,
		Spec:    map[string]interface{}{"task": "fix it", "id": id},
	}); err != nil {
		t.Fatalf("EnqueueTask(%s): %v", id, err)
	}
}

// A task pinned to fleet A must not be leased by a daemon in fleet B, and must
// be leased by a daemon in fleet A.
func TestLeaseRoutesByFleet(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	enqueueFleetTask(t, s, "t-a", "o1", "fleet-a")

	// A daemon in the wrong fleet sees no work.
	if l, err := s.LeaseNextTask(ctx, "o1", "d-b", "fleet-b", time.Minute); err != nil {
		t.Fatalf("LeaseNextTask(fleet-b): %v", err)
	} else if l != nil {
		t.Fatalf("fleet-b daemon must not lease a fleet-a task, got %s", l.ID)
	}

	// The right fleet's daemon leases it.
	l, err := s.LeaseNextTask(ctx, "o1", "d-a", "fleet-a", time.Minute)
	if err != nil {
		t.Fatalf("LeaseNextTask(fleet-a): %v", err)
	}
	if l == nil || l.ID != "t-a" {
		t.Fatalf("fleet-a daemon should lease t-a, got %v", l)
	}
}

// An unassigned task (fleet_id = "") runs on any daemon regardless of its fleet,
// so a task submitted without a fleet is never stranded.
func TestLeaseUnassignedTaskRunsOnAnyFleet(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	enqueueFleetTask(t, s, "t-open", "o1", "")

	l, err := s.LeaseNextTask(ctx, "o1", "d-a", "fleet-a", time.Minute)
	if err != nil {
		t.Fatalf("LeaseNextTask: %v", err)
	}
	if l == nil || l.ID != "t-open" {
		t.Fatalf("a fleet daemon should lease an unassigned task, got %v", l)
	}
}

// A daemon with no fleet (fleetID = "") leases only unassigned work — it must
// not pick up a task pinned to a specific fleet.
func TestLeaseFleetlessDaemonOnlyTakesUnassigned(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	enqueueFleetTask(t, s, "t-pinned", "o1", "fleet-a")

	if l, err := s.LeaseNextTask(ctx, "o1", "d0", "", time.Minute); err != nil {
		t.Fatalf("LeaseNextTask: %v", err)
	} else if l != nil {
		t.Fatalf("fleetless daemon must not lease a fleet-pinned task, got %s", l.ID)
	}

	// But it does take an unassigned one.
	enqueueFleetTask(t, s, "t-open", "o1", "")
	l, err := s.LeaseNextTask(ctx, "o1", "d0", "", time.Minute)
	if err != nil {
		t.Fatalf("LeaseNextTask: %v", err)
	}
	if l == nil || l.ID != "t-open" {
		t.Fatalf("fleetless daemon should lease the unassigned task, got %v", l)
	}
}
