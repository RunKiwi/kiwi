package store

import (
	"context"
	"strings"
	"testing"
	"time"
)

// provisioningRequestRow mirrors auth.ProvisioningRequest just enough to create
// the table in the test DB. store cannot import auth (auth imports store), which
// is also why DiagnoseQueuedTasks reads the table by name.
type provisioningRequestRow struct {
	ID        string `gorm:"primaryKey"`
	OrgID     string `gorm:"index;not null"`
	Type      string `gorm:"not null"`
	Status    string `gorm:"not null;default:pending"`
	Error     string
	CreatedAt time.Time
	UpdatedAt time.Time
}

func (provisioningRequestRow) TableName() string { return "provisioning_requests" }

// freeFleet mirrors auth.SharedFreeFleet, which store cannot import.
const freeFleet = "shared-free"

// diagStore is a test store with the provisioning table present, so the
// cold-start branches of the diagnosis are exercisable.
func diagStore(t *testing.T) *PostgresStore {
	t.Helper()
	s := newTestStore(t)
	if err := s.db.AutoMigrate(&provisioningRequestRow{}); err != nil {
		t.Fatalf("migrate provisioning_requests: %v", err)
	}
	return s
}

func enqueueDiagTask(t *testing.T, s *PostgresStore, id, org, fleet string, spec map[string]interface{}) {
	t.Helper()
	if spec == nil {
		spec = map[string]interface{}{"task": "fix it"}
	}
	err := s.EnqueueTask(context.Background(), &QueuedTask{
		ID: id, OrgID: org, JobID: "job1", FleetID: fleet, Spec: spec,
	})
	if err != nil {
		t.Fatalf("EnqueueTask(%s): %v", id, err)
	}
}

// registerDaemon inserts a daemon row directly; the join-token path is covered
// by daemons_test.go and is not what these tests are about.
func registerDaemon(t *testing.T, s *PostgresStore, id, org, fleet string, lastSeen *time.Time) {
	t.Helper()
	d := Daemon{
		ID: id, OrgID: org, FleetID: fleet,
		SignPubKey: "sign-" + id, EncPubKey: "enc-" + id,
		LastSeenAt: lastSeen,
	}
	if err := s.db.Create(&d).Error; err != nil {
		t.Fatalf("create daemon %s: %v", id, err)
	}
}

func diagnose(t *testing.T, s *PostgresStore, org string) map[string]TaskDiagnosis {
	t.Helper()
	var tasks []QueuedTask
	if err := s.db.Where("org_id = ?", org).Find(&tasks).Error; err != nil {
		t.Fatalf("load tasks: %v", err)
	}
	got, err := s.DiagnoseQueuedTasks(context.Background(), org, tasks)
	if err != nil {
		t.Fatalf("DiagnoseQueuedTasks: %v", err)
	}
	return got
}

func wantReason(t *testing.T, got map[string]TaskDiagnosis, id, reason string) TaskDiagnosis {
	t.Helper()
	d, ok := got[id]
	if !ok {
		t.Fatalf("no diagnosis for task %s (got %v)", id, got)
	}
	if d.Reason != reason {
		t.Fatalf("task %s: reason = %q (detail %q), want %q", id, d.Reason, d.Detail, reason)
	}
	return d
}

// The case that motivated all of this: the org has no daemon at all, so nothing
// can ever lease the task and the UI must say so instead of spinning.
func TestDiagnoseNoRunner(t *testing.T) {
	s := diagStore(t)
	enqueueDiagTask(t, s, "t1", "org1", freeFleet, nil)

	d := wantReason(t, diagnose(t, s, "org1"), "t1", BlockNoRunner)
	if d.Detail == "" {
		t.Error("no_runner should carry a human-readable detail")
	}
}

// A cold-start in flight is the benign explanation and must win over "no runner":
// the runner is coming, so the user is told to wait rather than told it's broken.
func TestDiagnoseProvisioningInFlight(t *testing.T) {
	s := diagStore(t)
	enqueueDiagTask(t, s, "t1", "org1", freeFleet, nil)
	if err := s.db.Create(&provisioningRequestRow{
		ID: "prov1", OrgID: "org1", Type: "provision", Status: "pending",
		CreatedAt: time.Now().Add(-90 * time.Second),
	}).Error; err != nil {
		t.Fatalf("create provisioning request: %v", err)
	}

	d := wantReason(t, diagnose(t, s, "org1"), "t1", BlockProvisioning)
	if !strings.Contains(d.Detail, "2m") {
		t.Errorf("detail should report how long the cold-start has been running, got %q", d.Detail)
	}
}

// A failed cold-start is terminal — nothing retries it — so the failure reason
// must reach the user rather than dying in a log on the provisioning host.
func TestDiagnoseProvisionFailedSurfacesError(t *testing.T) {
	s := diagStore(t)
	enqueueDiagTask(t, s, "t1", "org1", freeFleet, nil)
	if err := s.db.Create(&provisioningRequestRow{
		ID: "prov1", OrgID: "org1", Type: "provision", Status: "failed",
		Error:     "launch daemon: manifest unknown",
		CreatedAt: time.Now().Add(-time.Minute),
	}).Error; err != nil {
		t.Fatalf("create provisioning request: %v", err)
	}

	d := wantReason(t, diagnose(t, s, "org1"), "t1", BlockProvisionFailed)
	if !strings.Contains(d.Detail, "manifest unknown") {
		t.Errorf("detail should quote the recorded provisioning error, got %q", d.Detail)
	}
}

// A newer pending request behind a failed one means recovery is already under
// way; the stale failure must not be reported as the current state.
func TestDiagnosePendingWinsOverOlderFailure(t *testing.T) {
	s := diagStore(t)
	enqueueDiagTask(t, s, "t1", "org1", freeFleet, nil)
	rows := []provisioningRequestRow{
		{ID: "old", OrgID: "org1", Type: "provision", Status: "failed", Error: "boom", CreatedAt: time.Now().Add(-10 * time.Minute)},
		{ID: "new", OrgID: "org1", Type: "provision", Status: "pending", CreatedAt: time.Now().Add(-time.Minute)},
	}
	if err := s.db.Create(&rows).Error; err != nil {
		t.Fatalf("create provisioning requests: %v", err)
	}

	wantReason(t, diagnose(t, s, "org1"), "t1", BlockProvisioning)
}

// A registered daemon that stopped heartbeating is a different failure from
// never having had one, and points at a different fix.
func TestDiagnoseRunnerOffline(t *testing.T) {
	s := diagStore(t)
	enqueueDiagTask(t, s, "t1", "org1", freeFleet, nil)
	stale := time.Now().Add(-10 * time.Minute)
	registerDaemon(t, s, "d1", "org1", freeFleet, &stale)

	wantReason(t, diagnose(t, s, "org1"), "t1", BlockRunnerOffline)
}

// A live daemon on the fleet means the task is simply about to be picked up.
func TestDiagnoseAwaitingRunner(t *testing.T) {
	s := diagStore(t)
	enqueueDiagTask(t, s, "t1", "org1", freeFleet, nil)
	now := time.Now()
	registerDaemon(t, s, "d1", "org1", freeFleet, &now)

	wantReason(t, diagnose(t, s, "org1"), "t1", BlockAwaitingRunner)
}

// Fleet routing must be mirrored exactly: a live daemon on another fleet cannot
// lease this task, so it does not count as a runner for it.
func TestDiagnoseIgnoresOtherFleetsDaemon(t *testing.T) {
	s := diagStore(t)
	enqueueDiagTask(t, s, "t1", "org1", freeFleet, nil)
	now := time.Now()
	registerDaemon(t, s, "d1", "org1", "fleet-other", &now)

	wantReason(t, diagnose(t, s, "org1"), "t1", BlockNoRunner)
}

// An unassigned task (fleet_id = "") runs on any daemon, matching LeaseNextTask.
func TestDiagnoseUnassignedTaskAcceptsAnyDaemon(t *testing.T) {
	s := diagStore(t)
	enqueueDiagTask(t, s, "t1", "org1", "", nil)
	now := time.Now()
	registerDaemon(t, s, "d1", "org1", "fleet-other", &now)

	wantReason(t, diagnose(t, s, "org1"), "t1", BlockAwaitingRunner)
}

// The concurrency cap silently returns (nil, nil) from LeaseNextTask; the
// diagnosis is the only place that difference becomes visible.
func TestDiagnoseConcurrencyCap(t *testing.T) {
	s := diagStore(t)
	if err := s.db.Create(&OrgLimits{OrgID: "org1", MaxConcurrentJobs: 1, MaxBudgetPerJob: 5}).Error; err != nil {
		t.Fatalf("create limits: %v", err)
	}
	now := time.Now()
	registerDaemon(t, s, "d1", "org1", freeFleet, &now)

	enqueueDiagTask(t, s, "running", "org1", freeFleet, nil)
	if err := s.db.Model(&QueuedTask{}).Where("id = ?", "running").
		Update("status", TaskLeased).Error; err != nil {
		t.Fatalf("lease task: %v", err)
	}
	enqueueDiagTask(t, s, "t1", "org1", freeFleet, nil)

	got := diagnose(t, s, "org1")
	wantReason(t, got, "t1", BlockConcurrencyCap)
	if _, ok := got["running"]; ok {
		t.Error("a LEASED task should not be diagnosed")
	}
}

func TestDiagnoseComputeCap(t *testing.T) {
	s := diagStore(t)
	if err := s.db.Create(&OrgLimits{
		OrgID: "org1", MaxConcurrentJobs: 10, MaxAgentMinutesPerMonth: 60,
	}).Error; err != nil {
		t.Fatalf("create limits: %v", err)
	}
	if err := s.db.Create(&Job{
		ID: "j-used", OrgID: "org1", UserID: "u1", Status: "SUCCEEDED", AgentMinutes: 75,
	}).Error; err != nil {
		t.Fatalf("create job: %v", err)
	}
	now := time.Now()
	registerDaemon(t, s, "d1", "org1", freeFleet, &now)
	enqueueDiagTask(t, s, "t1", "org1", freeFleet, nil)

	d := wantReason(t, diagnose(t, s, "org1"), "t1", BlockComputeCap)
	if !strings.Contains(d.Detail, "60") {
		t.Errorf("detail should state the allowance, got %q", d.Detail)
	}
}

// A task held back by the DAG is not stuck at all, and must not be reported as
// an infrastructure problem — even when there is no runner to be seen.
func TestDiagnoseWaitingOnDependencies(t *testing.T) {
	s := diagStore(t)
	enqueueDiagTask(t, s, "job1-impl", "org1", freeFleet, nil)
	enqueueDiagTask(t, s, "job1-verify", "org1", freeFleet,
		map[string]interface{}{"task": "verify", "depends_on": []interface{}{"impl"}})

	got := diagnose(t, s, "org1")
	d := wantReason(t, got, "job1-verify", BlockWaitingDependencies)
	if !strings.Contains(d.Detail, "impl") {
		t.Errorf("detail should name the unmet dependency, got %q", d.Detail)
	}
	// The dependency itself is blocked on infrastructure, not on the DAG.
	wantReason(t, got, "job1-impl", BlockNoRunner)
}

// Terminal tasks have nothing to explain and are omitted entirely.
func TestDiagnoseSkipsTerminalTasks(t *testing.T) {
	s := diagStore(t)
	enqueueDiagTask(t, s, "done", "org1", freeFleet, nil)
	if err := s.db.Model(&QueuedTask{}).Where("id = ?", "done").
		Update("status", TaskSucceeded).Error; err != nil {
		t.Fatalf("complete task: %v", err)
	}

	if got := diagnose(t, s, "org1"); len(got) != 0 {
		t.Errorf("expected no diagnoses, got %v", got)
	}
}

// The diagnosis must never be the reason a status request fails, so a missing
// provisioning table degrades to a less specific answer rather than an error.
func TestDiagnoseWithoutProvisioningTable(t *testing.T) {
	s := newTestStore(t) // no provisioning_requests
	enqueueDiagTask(t, s, "t1", "org1", freeFleet, nil)

	wantReason(t, diagnose(t, s, "org1"), "t1", BlockNoRunner)
}
