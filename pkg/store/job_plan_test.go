package store

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestJobPlanModeColumnsRoundTrip(t *testing.T) {
	s := newTestStore(t)
	job := &Job{
		ID:                   "job-plan-test",
		OrgID:                "org-1",
		UserID:               "usr-1",
		Status:               "PENDING",
		Inputs:               map[string]interface{}{"task": "test"},
		RequiresPlanApproval: true,
		PlanStatus:           "drafting",
		ArchitectModel:       "claude-opus-4-8",
		WorkerModel:          "claude-haiku-4-5",
		SpendCapUSD:          0.75,
	}
	require.NoError(t, s.CreateJobWithOutbox(context.Background(), job, &Outbox{
		ID: 0, JobID: job.ID, Topic: "job.created", Payload: map[string]interface{}{},
	}))

	fetched, err := s.GetJob(context.Background(), "job-plan-test")
	require.NoError(t, err)
	require.True(t, fetched.RequiresPlanApproval)
	require.Equal(t, "drafting", fetched.PlanStatus)
	require.Equal(t, 0.75, fetched.SpendCapUSD)
	require.Equal(t, "claude-opus-4-8", fetched.ArchitectModel)
	require.Empty(t, fetched.PlanMarkdown)
}
