package store

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func newPlanTestJob(t *testing.T, s *PostgresStore, id string) {
	t.Helper()
	require.NoError(t, s.CreateJobWithOutbox(context.Background(), &Job{
		ID: id, OrgID: "org-1", UserID: "usr-1", Status: "RUNNING",
		Inputs: map[string]interface{}{"task": "test"}, RequiresPlanApproval: true,
	}, &Outbox{JobID: id, Topic: "job.created", Payload: map[string]interface{}{}}))
}

func TestSetJobPlanPendingReview(t *testing.T) {
	s := newTestStore(t)
	newPlanTestJob(t, s, "job-p1")
	require.NoError(t, s.SetJobPlanPendingReview(context.Background(), "job-p1", "# Plan\n1. Do the thing"))
	j, err := s.GetJob(context.Background(), "job-p1")
	require.NoError(t, err)
	require.Equal(t, "pending_review", j.PlanStatus)
	require.Equal(t, "# Plan\n1. Do the thing", j.PlanMarkdown)
	require.Equal(t, "PLAN_REVIEW", j.Status)
}

func TestApproveJobPlan(t *testing.T) {
	s := newTestStore(t)
	newPlanTestJob(t, s, "job-p2")
	require.NoError(t, s.SetJobPlanPendingReview(context.Background(), "job-p2", "plan text"))
	require.NoError(t, s.ApproveJobPlan(context.Background(), "job-p2"))
	j, err := s.GetJob(context.Background(), "job-p2")
	require.NoError(t, err)
	require.Equal(t, "approved", j.PlanStatus)
	require.Equal(t, "RUNNING", j.Status)
	require.NotNil(t, j.PlanAcceptedAt)
}

func TestRejectJobPlanAndRequestRevision(t *testing.T) {
	s := newTestStore(t)
	newPlanTestJob(t, s, "job-p5")
	require.NoError(t, s.SetJobPlanPendingReview(context.Background(), "job-p5", "plan text"))

	continuation := &QueuedTask{
		ID:         "job-p5-c1",
		OrgID:      "org-1",
		JobID:      "job-p5",
		Origin:     OriginPlanRevision,
		Status:     TaskQueued,
		RootTaskID: "job-p5-c1",
		Spec:       map[string]interface{}{"revision_feedback": "use CockroachDB leases instead"},
	}
	require.NoError(t, s.RejectJobPlanAndRequestRevision(context.Background(), "job-p5", "use CockroachDB leases instead", continuation))

	j, err := s.GetJob(context.Background(), "job-p5")
	require.NoError(t, err)
	require.Equal(t, "", j.PlanStatus, "must reset, not settle on a terminal status, so the daemon's next SetJobPlanPendingReview can fire")
	require.Equal(t, "RUNNING", j.Status)
	require.Equal(t, "use CockroachDB leases instead", j.PlanRejectedReason)

	tasks, err := s.GetJobTasks(context.Background(), "org-1", "job-p5")
	require.NoError(t, err)
	require.Len(t, tasks, 1)
	require.Equal(t, OriginPlanRevision, tasks[0].Origin)
}

func TestRejectJobPlanAndRequestRevisionConflictWhenNotPendingReview(t *testing.T) {
	s := newTestStore(t)
	newPlanTestJob(t, s, "job-p6")
	// Never entered pending_review.
	continuation := &QueuedTask{ID: "job-p6-c1", OrgID: "org-1", JobID: "job-p6", Origin: OriginPlanRevision, RootTaskID: "job-p6-c1"}

	err := s.RejectJobPlanAndRequestRevision(context.Background(), "job-p6", "feedback", continuation)
	require.ErrorIs(t, err, ErrPlanStatusConflict)

	tasks, terr := s.GetJobTasks(context.Background(), "org-1", "job-p6")
	require.NoError(t, terr)
	require.Empty(t, tasks, "no continuation must be created on conflict")
}

func TestSetJobSpendCap(t *testing.T) {
	s := newTestStore(t)
	newPlanTestJob(t, s, "job-p4")

	// Success for matching org
	require.NoError(t, s.SetJobSpendCap(context.Background(), "org-1", "job-p4", 2.50))
	j, err := s.GetJob(context.Background(), "job-p4")
	require.NoError(t, err)
	require.Equal(t, 2.50, j.SpendCapUSD)

	// Failure for non-matching org
	err = s.SetJobSpendCap(context.Background(), "org-2", "job-p4", 1.50)
	require.ErrorIs(t, err, ErrJobNotFound)

	// Failure for non-existent job
	err = s.SetJobSpendCap(context.Background(), "org-1", "job-non-existent", 1.50)
	require.ErrorIs(t, err, ErrJobNotFound)
}
