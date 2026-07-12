package store

import (
	"context"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

// newTestStore spins up an isolated in-memory SQLite Store with the V2 schema
// migrated, so store behavior can be verified without a real Postgres/network.
func newTestStore(t *testing.T) *PostgresStore {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(
		&Organization{}, &OrgLimits{}, &Job{}, &Outbox{},
		&Event{}, &Checkpoint{}, &SideEffect{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return NewPostgresStore(db)
}

func newJob(id, org string, key *string) *Job {
	return &Job{
		ID: id, OrgID: org, UserID: "u1", Status: "PENDING",
		IdempotencyKey: key,
		Inputs:         map[string]interface{}{"task": "fix it"},
	}
}

// P1.2 spine: job + outbox are written atomically, and the outbox row is
// created unpublished for the relay to pick up.
func TestCreateJobWithOutbox(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	job := newJob("j1", "o1", nil)
	ob := &Outbox{JobID: "j1", Topic: "jobs.submitted", Payload: map[string]interface{}{"job_id": "j1"}}
	if err := s.CreateJobWithOutbox(ctx, job, ob); err != nil {
		t.Fatalf("CreateJobWithOutbox: %v", err)
	}

	got, err := s.GetJob(ctx, "j1")
	if err != nil || got.Status != "PENDING" {
		t.Fatalf("GetJob: %+v err=%v", got, err)
	}

	var unpublished int64
	s.DB().Model(&Outbox{}).Where("job_id = ? AND published_at IS NULL", "j1").Count(&unpublished)
	if unpublished != 1 {
		t.Errorf("expected 1 unpublished outbox row, got %d", unpublished)
	}
}

// CP invariant: a status transition only applies from the expected state, so a
// job can never be double-scheduled under duplicate queue delivery.
func TestUpdateJobStatusConditional(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if err := s.CreateJobWithOutbox(ctx, newJob("j1", "o1", nil),
		&Outbox{JobID: "j1", Topic: "t", Payload: map[string]interface{}{}}); err != nil {
		t.Fatal(err)
	}

	ok, err := s.UpdateJobStatus(ctx, "j1", "PENDING", "RUNNING")
	if err != nil || !ok {
		t.Fatalf("first transition should apply: ok=%v err=%v", ok, err)
	}
	// A second consumer seeing the stale "PENDING" expectation must be a no-op.
	ok2, err := s.UpdateJobStatus(ctx, "j1", "PENDING", "SCHEDULING")
	if err != nil {
		t.Fatalf("second transition err: %v", err)
	}
	if ok2 {
		t.Error("stale-expected transition must NOT apply (no double-schedule)")
	}
	got, _ := s.GetJob(ctx, "j1")
	if got.Status != "RUNNING" {
		t.Errorf("status = %q, want RUNNING", got.Status)
	}
}

// Idempotent submission: two jobs with the same idempotency key are rejected by
// the unique index (the daemon returns the original instead of creating a dup).
func TestIdempotencyKeyUnique(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	key := "dup-key"

	if err := s.CreateJobWithOutbox(ctx, newJob("a", "o1", &key),
		&Outbox{JobID: "a", Topic: "t", Payload: map[string]interface{}{}}); err != nil {
		t.Fatalf("first create: %v", err)
	}
	err := s.CreateJobWithOutbox(ctx, newJob("b", "o1", &key),
		&Outbox{JobID: "b", Topic: "t", Payload: map[string]interface{}{}})
	if err == nil {
		t.Error("duplicate idempotency key must violate the unique index")
	}

	// Distinct keys (and nil keys) coexist.
	k2 := "other-key"
	if err := s.CreateJobWithOutbox(ctx, newJob("c", "o1", &k2),
		&Outbox{JobID: "c", Topic: "t", Payload: map[string]interface{}{}}); err != nil {
		t.Errorf("distinct key should succeed: %v", err)
	}

	// Dedupe is per-tenant: the SAME key in a DIFFERENT org must NOT collide.
	if err := s.CreateJobWithOutbox(ctx, newJob("d", "o2", &key),
		&Outbox{JobID: "d", Topic: "t", Payload: map[string]interface{}{}}); err != nil {
		t.Errorf("same key in a different org must be allowed (per-org scoping): %v", err)
	}
}

// Side-effect ledger: replay-safety hinges on recording effects and finding
// them again (a hit short-circuits a re-fire).
func TestSideEffectLedger(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	if _, err := s.GetSideEffect(ctx, "missing"); err == nil {
		t.Error("unrecorded effect should not be found")
	}
	if err := s.RecordSideEffect(ctx, &SideEffect{ID: "e1", JobID: "j1", EffectType: "http"}); err != nil {
		t.Fatalf("RecordSideEffect: %v", err)
	}
	got, err := s.GetSideEffect(ctx, "e1")
	if err != nil || got.EffectType != "http" {
		t.Fatalf("GetSideEffect: %+v err=%v", got, err)
	}
}

// Cost accrues additively for the budget gate.
func TestUpdateJobCost(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if err := s.CreateJobWithOutbox(ctx, newJob("j1", "o1", nil),
		&Outbox{JobID: "j1", Topic: "t", Payload: map[string]interface{}{}}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateJobCost(ctx, "j1", 0.05); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateJobCost(ctx, "j1", 0.05); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetJob(ctx, "j1")
	if got.CostUSD < 0.099 || got.CostUSD > 0.101 {
		t.Errorf("cost = %v, want ~0.10", got.CostUSD)
	}
}
