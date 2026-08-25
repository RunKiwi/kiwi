package store

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func seedTask(t *testing.T, s *PostgresStore, id, org, job, status string) {
	t.Helper()
	if err := s.db.Create(&QueuedTask{
		ID: id, OrgID: org, JobID: job, Status: status,
		Spec:      map[string]interface{}{"task": "fix it"},
		CreatedAt: time.Now(),
	}).Error; err != nil {
		t.Fatalf("seed %s: %v", id, err)
	}
}

func statusOfTask(t *testing.T, s *PostgresStore, id string) QueuedTask {
	t.Helper()
	var got QueuedTask
	if err := s.db.First(&got, "id = ?", id).Error; err != nil {
		t.Fatalf("load %s: %v", id, err)
	}
	return got
}

// Cancel must stop pending work and revoke an in-flight lease, while leaving
// already-terminal tasks exactly as they were.
func TestCancelJob(t *testing.T) {
	s := newTestStore(t)
	seedTask(t, s, "j1-a", "org1", "j1", TaskQueued)
	seedTask(t, s, "j1-b", "org1", "j1", TaskLeased)
	seedTask(t, s, "j1-c", "org1", "j1", TaskSucceeded)

	n, err := s.CancelJob(context.Background(), "org1", "j1", "cancelled by user")
	if err != nil {
		t.Fatalf("CancelJob: %v", err)
	}
	if n != 2 {
		t.Fatalf("affected: got %d, want 2 (queued + leased)", n)
	}

	if got := statusOfTask(t, s, "j1-a"); got.Status != TaskCancelled {
		t.Errorf("queued task: got %s, want CANCELLED", got.Status)
	}
	leased := statusOfTask(t, s, "j1-b")
	if leased.Status != TaskCancelled {
		t.Errorf("leased task: got %s, want CANCELLED", leased.Status)
	}
	// The lease must be cleared, or the expiry sweeper has a terminal row with a
	// live-looking fencing token to reason about.
	if leased.LeaseID != nil || leased.LeasedBy != nil || leased.LeaseExpiresAt != nil {
		t.Errorf("cancelled task should have no lease: %+v", leased)
	}
	if got := statusOfTask(t, s, "j1-c"); got.Status != TaskSucceeded {
		t.Errorf("a finished task must not be cancelled, got %s", got.Status)
	}
}

// A job parked in Plan Mode review has no QUEUED or LEASED task — its one
// task is PLAN_REVIEW, which CancelJob must also treat as cancellable, or a
// plan a human never approves nor rejects can never be closed out either.
func TestCancelJobCancelsPlanReviewTask(t *testing.T) {
	s := newTestStore(t)
	newPlanTestJob(t, s, "job-cancel-pr")
	require.NoError(t, s.SetJobPlanPendingReview(context.Background(), "job-cancel-pr", "plan text"))
	seedTask(t, s, "job-cancel-pr-t1", "org-1", "job-cancel-pr", TaskPlanReview)

	n, err := s.CancelJob(context.Background(), "org-1", "job-cancel-pr", "cancelled by user")
	require.NoError(t, err)
	require.Equal(t, 1, n)

	require.Equal(t, TaskCancelled, statusOfTask(t, s, "job-cancel-pr-t1").Status)

	j, err := s.GetJob(context.Background(), "job-cancel-pr")
	require.NoError(t, err)
	require.Equal(t, "CANCELLED", j.Status)
	require.Equal(t, "", j.PlanStatus, "must clear, or a stale PlanApprovalCard keeps showing after cancel")
}

// Cancellation is org-scoped: another tenant's job id changes nothing and does
// not report an error that would confirm the id exists.
func TestCancelJobIsOrgScoped(t *testing.T) {
	s := newTestStore(t)
	seedTask(t, s, "j1-a", "org1", "j1", TaskQueued)

	n, err := s.CancelJob(context.Background(), "org2", "j1", "")
	if err != nil {
		t.Fatalf("CancelJob: %v", err)
	}
	if n != 0 {
		t.Errorf("cross-org cancel affected %d tasks, want 0", n)
	}
	if got := statusOfTask(t, s, "j1-a"); got.Status != TaskQueued {
		t.Errorf("task should be untouched, got %s", got.Status)
	}
}

// A cancelled task's daemon may still report a result. The fencing token must
// reject it, or a cancelled job could resurrect itself.
func TestCompleteTaskRejectsCancelledTask(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if err := s.EnqueueTask(ctx, &QueuedTask{
		ID: "j1-a", OrgID: "org1", JobID: "j1",
		Spec: map[string]interface{}{"task": "fix it"},
	}); err != nil {
		t.Fatal(err)
	}
	registerDaemon(t, s, "d1", "org1", "", nil)
	leased, err := s.LeaseNextTask(ctx, "org1", "d1", "", time.Minute)
	if err != nil || leased == nil {
		t.Fatalf("lease: %v %v", leased, err)
	}
	leaseID := *leased.LeaseID

	if _, err := s.CancelJob(ctx, "org1", "j1", ""); err != nil {
		t.Fatalf("CancelJob: %v", err)
	}

	ok, err := s.CompleteTask(ctx, TaskCompletion{TaskID: "j1-a", LeaseID: leaseID, FinalStatus: TaskSucceeded, ResultURL: "https://pr"})
	if err != nil {
		t.Fatalf("CompleteTask: %v", err)
	}
	if ok {
		t.Error("a late completion must not revive a cancelled task")
	}
	if got := statusOfTask(t, s, "j1-a"); got.Status != TaskCancelled {
		t.Errorf("task should still be CANCELLED, got %s", got.Status)
	}
}

// Retry requeues only the unsuccessful tasks, and resets the attempt count so
// the poison-pill guard does not immediately dead-letter the retried work.
func TestRetryJob(t *testing.T) {
	s := newTestStore(t)
	seedTask(t, s, "j1-a", "org1", "j1", TaskFailed)
	seedTask(t, s, "j1-b", "org1", "j1", TaskCancelled)
	seedTask(t, s, "j1-c", "org1", "j1", TaskSucceeded)

	detail := "it broke"
	url := "https://old-pr"
	if err := s.db.Model(&QueuedTask{}).Where("id = ?", "j1-a").
		Updates(map[string]interface{}{
			"attempts": MaxLeaseAttempts, "result_detail": &detail, "result_url": &url,
		}).Error; err != nil {
		t.Fatal(err)
	}

	n, err := s.RetryJob(context.Background(), "org1", "j1")
	if err != nil {
		t.Fatalf("RetryJob: %v", err)
	}
	if n != 2 {
		t.Fatalf("affected: got %d, want 2 (failed + cancelled)", n)
	}

	a := statusOfTask(t, s, "j1-a")
	if a.Status != TaskQueued {
		t.Errorf("failed task: got %s, want QUEUED", a.Status)
	}
	if a.Attempts != 0 {
		t.Errorf("attempts should reset to 0, got %d — otherwise MaxLeaseAttempts dead-letters it at once", a.Attempts)
	}
	// A stale failure reason or PR link would be read as belonging to the new run.
	if a.ResultDetail != nil || a.ResultURL != nil {
		t.Errorf("previous result should be cleared, got detail=%v url=%v", a.ResultDetail, a.ResultURL)
	}
	if got := statusOfTask(t, s, "j1-b"); got.Status != TaskQueued {
		t.Errorf("cancelled task: got %s, want QUEUED", got.Status)
	}
	if got := statusOfTask(t, s, "j1-c"); got.Status != TaskSucceeded {
		t.Errorf("a succeeded task must not be re-run, got %s", got.Status)
	}
}

func TestDeleteJob(t *testing.T) {
	s := newTestStore(t)
	seedTask(t, s, "j1-a", "org1", "j1", TaskFailed)
	seedTask(t, s, "j2-a", "org1", "j2", TaskQueued)

	n, err := s.DeleteJob(context.Background(), "org1", "j1")
	if err != nil {
		t.Fatalf("DeleteJob: %v", err)
	}
	if n != 1 {
		t.Fatalf("deleted: got %d, want 1", n)
	}

	var remaining int64
	s.db.Model(&QueuedTask{}).Where("job_id = ?", "j1").Count(&remaining)
	if remaining != 0 {
		t.Errorf("job j1 should be gone, %d tasks remain", remaining)
	}
	s.db.Model(&QueuedTask{}).Where("job_id = ?", "j2").Count(&remaining)
	if remaining != 1 {
		t.Errorf("unrelated job must survive, got %d tasks", remaining)
	}
}

func TestDeleteJobIsOrgScoped(t *testing.T) {
	s := newTestStore(t)
	seedTask(t, s, "j1-a", "org1", "j1", TaskQueued)

	n, err := s.DeleteJob(context.Background(), "org2", "j1")
	if err != nil {
		t.Fatalf("DeleteJob: %v", err)
	}
	if n != 0 {
		t.Errorf("cross-org delete removed %d tasks, want 0", n)
	}
	if got := statusOfTask(t, s, "j1-a"); got.Status != TaskQueued {
		t.Errorf("task should survive, got %s", got.Status)
	}
}

// A cancelled task is terminal: it must not be leased, nor reported as blocked.
func TestCancelledTaskIsNotLeasableOrDiagnosed(t *testing.T) {
	s := diagStore(t)
	ctx := context.Background()
	seedTask(t, s, "j1-a", "org1", "j1", TaskQueued)
	if _, err := s.CancelJob(ctx, "org1", "j1", ""); err != nil {
		t.Fatal(err)
	}
	registerDaemon(t, s, "d1", "org1", "", nil)

	leased, err := s.LeaseNextTask(ctx, "org1", "d1", "", time.Minute)
	if err != nil {
		t.Fatalf("LeaseNextTask: %v", err)
	}
	if leased != nil {
		t.Errorf("a cancelled task must never be leased, got %s", leased.ID)
	}

	if got := diagnose(t, s, "org1"); len(got) != 0 {
		t.Errorf("terminal task should not be diagnosed, got %v", got)
	}
}

// The job-list rollup must call a cancelled job cancelled rather than letting a
// half-finished cancel read as QUEUED or SUCCEEDED.
func TestListJobsReportsCancelled(t *testing.T) {
	s := newTestStore(t)
	seedTask(t, s, "j1-a", "org1", "j1", TaskCancelled)
	seedTask(t, s, "j1-b", "org1", "j1", TaskSucceeded)

	jobs, err := s.ListJobs(context.Background(), "org1")
	if err != nil {
		t.Fatalf("ListJobs: %v", err)
	}
	if len(jobs) != 1 {
		t.Fatalf("jobs: got %d, want 1", len(jobs))
	}
	if jobs[0].Status != "CANCELLED" {
		t.Errorf("status: got %s, want CANCELLED", jobs[0].Status)
	}
}

// A genuine failure is more important to surface than a cancellation.
func TestListJobsFailedOutranksCancelled(t *testing.T) {
	s := newTestStore(t)
	seedTask(t, s, "j1-a", "org1", "j1", TaskCancelled)
	seedTask(t, s, "j1-b", "org1", "j1", TaskFailed)

	jobs, err := s.ListJobs(context.Background(), "org1")
	if err != nil {
		t.Fatalf("ListJobs: %v", err)
	}
	if jobs[0].Status != "FAILED" {
		t.Errorf("status: got %s, want FAILED", jobs[0].Status)
	}
}

func TestHasActiveTasks(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	active, err := s.HasActiveTasks(ctx, "")
	if err != nil {
		t.Fatalf("HasActiveTasks: %v", err)
	}
	if active {
		t.Error("empty queue should report no active tasks")
	}

	seedTask(t, s, "j1-a", "org1", "j1", TaskQueued)
	if active, _ = s.HasActiveTasks(ctx, ""); !active {
		t.Error("a QUEUED task is active work")
	}

	if _, err := s.CancelJob(ctx, "org1", "j1", ""); err != nil {
		t.Fatal(err)
	}
	if active, _ = s.HasActiveTasks(ctx, ""); active {
		t.Error("a cancelled task is terminal and must not count as active")
	}
}

// RecordTaskProgress must persist when the current phase started, not just
// what it is — the dashboard uses this to show how long a step has been
// running, distinct from ProgressAt (when the daemon last reported at all).
func TestRecordTaskProgress_PersistsPhaseSince(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if err := s.EnqueueTask(ctx, &QueuedTask{
		ID: "j1-a", OrgID: "org1", JobID: "j1",
		Spec: map[string]interface{}{"task": "fix it"},
	}); err != nil {
		t.Fatal(err)
	}
	registerDaemon(t, s, "d1", "org1", "", nil)
	leased, err := s.LeaseNextTask(ctx, "org1", "d1", "", time.Minute)
	if err != nil || leased == nil {
		t.Fatalf("lease: %v %v", leased, err)
	}
	leaseID := *leased.LeaseID

	since := time.Now().Add(-90 * time.Second).UTC().Truncate(time.Second)
	ok, err := s.RecordTaskProgress(ctx, "j1-a", leaseID, "install: npm ci", "", since)
	if err != nil {
		t.Fatalf("RecordTaskProgress: %v", err)
	}
	if !ok {
		t.Fatal("expected the write to apply")
	}

	got := statusOfTask(t, s, "j1-a")
	if got.ProgressPhaseSince == nil {
		t.Fatal("ProgressPhaseSince was not persisted")
	}
	if !got.ProgressPhaseSince.Equal(since) {
		t.Errorf("ProgressPhaseSince = %v, want %v", got.ProgressPhaseSince, since)
	}
}

// A phase update with a zero PhaseSince (the caller has nothing new to say
// about timing) must not overwrite a previously recorded value with NULL.
func TestRecordTaskProgress_ZeroPhaseSinceLeavesExistingValueAlone(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if err := s.EnqueueTask(ctx, &QueuedTask{
		ID: "j1-b", OrgID: "org1", JobID: "j1",
		Spec: map[string]interface{}{"task": "fix it"},
	}); err != nil {
		t.Fatal(err)
	}
	registerDaemon(t, s, "d2", "org1", "", nil)
	leased, err := s.LeaseNextTask(ctx, "org1", "d2", "", time.Minute)
	if err != nil || leased == nil {
		t.Fatalf("lease: %v %v", leased, err)
	}
	leaseID := *leased.LeaseID

	since := time.Now().UTC().Truncate(time.Second)
	if _, err := s.RecordTaskProgress(ctx, "j1-b", leaseID, "install: npm ci", "", since); err != nil {
		t.Fatalf("first RecordTaskProgress: %v", err)
	}
	if _, err := s.RecordTaskProgress(ctx, "j1-b", leaseID, "install: npm ci", "more output", time.Time{}); err != nil {
		t.Fatalf("second RecordTaskProgress: %v", err)
	}

	got := statusOfTask(t, s, "j1-b")
	if got.ProgressPhaseSince == nil || !got.ProgressPhaseSince.Equal(since) {
		t.Errorf("ProgressPhaseSince = %v, want unchanged %v", got.ProgressPhaseSince, since)
	}
}
