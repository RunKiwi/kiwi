// SPDX-License-Identifier: LicenseRef-Kiwi-BSL-1.1
// Copyright (c) 2026 RunKiwi. Licensed under the Business Source License 1.1.
// See ee/LICENSE. This is Control Plane code and is NOT Apache-2.0.

package orchestrator

import (
	"context"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/ibreakthecloud/kiwi/pkg/store"
)

func TestMaybeAssembleRecordPlanModeJob(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)

	require.NoError(t, db.AutoMigrate(
		&store.Organization{},
		&store.Job{},
		&store.QueuedTask{},
		&store.Manifest{},
		&TaskEvent{},
		&store.ExecutionRecord{},
		&store.ExecutionRecordHead{},
	))

	st := store.NewPostgresStore(db)
	s := &Server{db: db, storage: st}

	ctx := context.Background()
	orgID := "org-plan-ver-1"
	jobID := "job-plan-ver-1"

	require.NoError(t, db.Create(&store.Organization{ID: orgID, Name: "Plan Test Org"}).Error)

	job := &store.Job{
		ID:                   jobID,
		OrgID:                orgID,
		UserID:               "usr-1",
		Status:               "RUNNING",
		Inputs:               map[string]interface{}{"task": "refactor api client", "repo_url": "https://github.com/org/repo"},
		RequiresPlanApproval: true,
		PlanStatus:           "approved",
		CreatedAt:            time.Now().Add(-10 * time.Minute),
	}
	require.NoError(t, db.Create(job).Error)

	task1 := store.QueuedTask{
		ID:         "task-plan-1",
		OrgID:      orgID,
		JobID:      jobID,
		RootTaskID: "task-plan-1",
		Status:     store.TaskPlanReview,
		Spec:       map[string]interface{}{"worker_id": "w1", "model": "test-model"},
		CreatedAt:  time.Now().Add(-10 * time.Minute),
		UpdatedAt:  time.Now().Add(-5 * time.Minute),
	}
	require.NoError(t, st.EnqueueTask(ctx, &task1))

	task2 := store.QueuedTask{
		ID:           "task-plan-2",
		OrgID:        orgID,
		JobID:        jobID,
		ParentTaskID: &task1.ID,
		RootTaskID:   task1.ID,
		Origin:       store.OriginPlanApproved,
		Status:       store.TaskSucceeded,
		Spec:         map[string]interface{}{"worker_id": "w2", "model": "test-model"},
		CreatedAt:    time.Now().Add(-5 * time.Minute),
		UpdatedAt:    time.Now(),
	}
	require.NoError(t, st.EnqueueTask(ctx, &task2))

	ec := execContext{
		FleetID:        "fleet-1",
		DaemonID:       "daemon-1",
		SandboxRuntime: "docker",
		Mode:           "plan_mode",
	}

	s.maybeAssembleRecord(ctx, orgID, task2.ID, ec)

	rec, err := s.storage.GetExecutionRecord(ctx, orgID, job.ID)
	require.NoError(t, err)
	require.NotNil(t, rec)
	require.Equal(t, orgID, rec.OrgID)
	require.Equal(t, job.ID, rec.JobID)
}
