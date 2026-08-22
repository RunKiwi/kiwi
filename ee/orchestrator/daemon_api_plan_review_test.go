// SPDX-License-Identifier: LicenseRef-Kiwi-BSL-1.1
// Copyright (c) 2026 RunKiwi. Licensed under the Business Source License 1.1.
// See ee/LICENSE. This is Control Plane code and is NOT Apache-2.0.

package orchestrator

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ibreakthecloud/kiwi/pkg/daemon"
	"github.com/ibreakthecloud/kiwi/pkg/session"
	"github.com/ibreakthecloud/kiwi/pkg/store"
)

func TestHandleDaemonResultPlanReview(t *testing.T) {
	ts, st := newSeamTestServer(t)
	ctx := context.Background()

	require.NoError(t, st.DB().Create(&store.Organization{ID: "o1", Name: "Org One"}).Error)

	jobID := "job-plan-1"
	require.NoError(t, st.DB().Create(&store.Job{
		ID:                   jobID,
		OrgID:                "o1",
		UserID:               "usr-1",
		Status:               "RUNNING",
		Inputs:               map[string]interface{}{"task": "add retry logic"},
		RequiresPlanApproval: true,
	}).Error)

	taskID := "task-plan-1"
	require.NoError(t, st.EnqueueTask(ctx, &store.QueuedTask{
		ID:     taskID,
		OrgID:  "o1",
		JobID:  jobID,
		Status: store.TaskQueued,
		Spec:   map[string]interface{}{"id": taskID, "task": "add retry logic", "requires_plan_approval": true},
	}))

	d := newDaemonKeys(t, ts.URL)
	token, err := st.CreateDaemonJoinToken(ctx, "o1", "", time.Hour)
	require.NoError(t, err)
	require.NoError(t, d.client.Register(ctx, daemon.RegisterReq{
		JoinToken:  token,
		PubKey:     d.encPubB64(),
		SignPubKey: d.signPubB64(),
	}))

	res, err := d.client.Heartbeat(ctx, daemon.HeartbeatReq{
		PubKey:     d.encPubB64(),
		SignPubKey: d.signPubB64(),
		Timestamp:  time.Now().Unix(),
	})
	require.NoError(t, err)
	require.NotNil(t, res)
	require.Len(t, res.Specs, 1)
	require.NotEmpty(t, res.LeaseID)

	specJSON, err := json.Marshal(session.Spec{
		Objective:          "add retry logic",
		AcceptanceCriteria: []string{"retries three times", "backs off exponentially"},
		MustChange:         []string{"pkg/client/retry.go"},
	})
	require.NoError(t, err)

	err = d.client.ReportResult(ctx, daemon.ResultReq{
		TaskID:       res.Specs[0].ID,
		LeaseID:      res.LeaseID,
		Status:       "PLAN_REVIEW",
		SignPubKey:   d.signPubB64(),
		Detail:       "plan pending review",
		PlanSpecJSON: string(specJSON),
	})
	require.NoError(t, err)

	job, err := st.GetJob(ctx, jobID)
	require.NoError(t, err)
	require.Equal(t, "pending_review", job.PlanStatus)
	require.Equal(t, "PLAN_REVIEW", job.Status)
	require.Contains(t, job.PlanMarkdown, "add retry logic")
	require.Contains(t, job.PlanMarkdown, "retries three times")
}

func TestRenderPlanMarkdown(t *testing.T) {
	spec := session.Spec{
		Objective:          "Improve error handling",
		AcceptanceCriteria: []string{"returns wrapped error", "logs error details"},
		MustChange:         []string{"pkg/server/handler.go"},
		MustNotChange:      []string{"pkg/server/handler_test.go"},
		Rationale:          "Cleaner debugging in production.",
	}
	rendered := renderPlanMarkdown(spec)
	expected := "# Improve error handling\n\n" +
		"## Acceptance criteria\n" +
		"- returns wrapped error\n" +
		"- logs error details\n\n" +
		"## Files expected to change\n" +
		"- `pkg/server/handler.go`\n\n" +
		"## Must not change\n" +
		"- `pkg/server/handler_test.go`\n\n" +
		"## Rationale\n" +
		"Cleaner debugging in production.\n"

	require.Equal(t, expected, rendered)

	// Minimal spec
	minSpec := session.Spec{
		Objective: "Quick fix",
	}
	minRendered := renderPlanMarkdown(minSpec)
	require.Equal(t, "# Quick fix\n\n", minRendered)
}
