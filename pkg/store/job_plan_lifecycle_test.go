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

func TestRejectJobPlan(t *testing.T) {
	s := newTestStore(t)
	newPlanTestJob(t, s, "job-p3")
	require.NoError(t, s.SetJobPlanPendingReview(context.Background(), "job-p3", "plan text"))
	require.NoError(t, s.RejectJobPlan(context.Background(), "job-p3", "use CockroachDB leases instead"))
	j, err := s.GetJob(context.Background(), "job-p3")
	require.NoError(t, err)
	require.Equal(t, "rejected", j.PlanStatus)
	require.Equal(t, "FAILED", j.Status)
	require.Equal(t, "use CockroachDB leases instead", j.PlanRejectedReason)
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
