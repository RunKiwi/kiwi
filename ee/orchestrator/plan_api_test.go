// SPDX-License-Identifier: LicenseRef-Kiwi-BSL-1.1
// Copyright (c) 2026 RunKiwi. Licensed under the Business Source License 1.1.
// See ee/LICENSE. This is Control Plane code and is NOT Apache-2.0.

package orchestrator

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ibreakthecloud/kiwi/ee/auth"
	"github.com/ibreakthecloud/kiwi/pkg/store"
)

func newTestPlanServer(t *testing.T) (*Server, *http.ServeMux) {
	t.Helper()
	s := newTestServer(t)
	_ = s.db.AutoMigrate(&store.Outbox{})
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/jobs", s.handleJobsList)
	mux.HandleFunc("/api/v1/jobs/", s.handleJobStatus)
	return s, mux
}

func seedJobPendingReview(t *testing.T, s *Server, orgID, jobID string) *store.Job {
	t.Helper()
	job := &store.Job{
		ID:                   jobID,
		OrgID:                orgID,
		UserID:               "usr-1",
		Status:               "PLAN_REVIEW",
		Inputs:               map[string]interface{}{"task": "test"},
		RequiresPlanApproval: true,
		PlanStatus:           "pending_review",
		PlanMarkdown:         "# Plan\n1. Modify auth.go\n2. Add test",
		ArchitectModel:       "claude-3-7-sonnet",
	}
	require.NoError(t, s.storage.CreateJobWithOutbox(context.Background(), job, &store.Outbox{
		JobID: jobID, Topic: "job.created", Payload: map[string]interface{}{},
	}))
	return job
}

func seedJobPendingReviewWithLeasedTask(t *testing.T, s *Server, orgID, jobID, taskID string) *store.Job {
	t.Helper()
	job := seedJobPendingReview(t, s, orgID, jobID)
	task := &store.QueuedTask{
		ID:         taskID,
		OrgID:      orgID,
		JobID:      jobID,
		Status:     store.TaskPlanReview,
		RootTaskID: taskID,
		Origin:     store.OriginSubmit,
		Spec:       map[string]interface{}{"task": "test", "repo_url": "https://github.com/example/repo", "model": "claude-3-5-haiku"},
		FleetID:    "default",
	}
	require.NoError(t, s.storage.EnqueueTask(context.Background(), task))
	require.NoError(t, s.db.Model(&store.QueuedTask{}).Where("id = ?", taskID).Update("status", store.TaskPlanReview).Error)
	return job
}

func authedRequest(t *testing.T, method, path string, body io.Reader, orgID string) *http.Request {
	t.Helper()
	req := httptest.NewRequest(method, path, body)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req = req.WithContext(auth.ContextWithClaims(req.Context(), &auth.UserClaims{
		OrgID:  orgID,
		UserID: "usr-1",
		Email:  "test@example.com",
	}))
	return req
}

func TestHandleGetJobPlan(t *testing.T) {
	s, mux := newTestPlanServer(t)
	seedJobPendingReview(t, s, "org-1", "job-1")

	req := authedRequest(t, http.MethodGet, "/api/v1/jobs/job-1/plan", nil, "org-1")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var resp struct {
		JobID            string `json:"job_id"`
		PlanStatus       string `json:"plan_status"`
		PlanMarkdown     string `json:"plan_markdown"`
		RequiresApproval bool   `json:"requires_approval"`
		ArchitectModel   string `json:"architect_model"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Equal(t, "job-1", resp.JobID)
	require.Equal(t, "pending_review", resp.PlanStatus)
	require.Equal(t, "# Plan\n1. Modify auth.go\n2. Add test", resp.PlanMarkdown)
	require.True(t, resp.RequiresApproval)
	require.Equal(t, "claude-3-7-sonnet", resp.ArchitectModel)
}

func TestHandleGetJobPlanForbidsOtherOrg(t *testing.T) {
	s, mux := newTestPlanServer(t)
	seedJobPendingReview(t, s, "org-1", "job-2")

	req := authedRequest(t, http.MethodGet, "/api/v1/jobs/job-2/plan", nil, "org-2")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	require.Equal(t, http.StatusForbidden, w.Code)
}

func TestHandleGetJobPlanNotFound(t *testing.T) {
	_, mux := newTestPlanServer(t)

	req := authedRequest(t, http.MethodGet, "/api/v1/jobs/nonexistent/plan", nil, "org-1")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	require.Equal(t, http.StatusNotFound, w.Code)
}

func TestHandleApproveJobPlanEnqueuesContinuation(t *testing.T) {
	s, mux := newTestPlanServer(t)
	seedJobPendingReviewWithLeasedTask(t, s, "org-1", "job-3", "task-3a")

	body, _ := json.Marshal(map[string]string{"user_comment": "looks right"})
	req := authedRequest(t, http.MethodPost, "/api/v1/jobs/job-3/plan/approve", bytes.NewReader(body), "org-1")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var resp struct {
		Status       string `json:"status"`
		ResumedPhase string `json:"resumed_phase"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Equal(t, "approved", resp.Status)
	require.Equal(t, "actor", resp.ResumedPhase)

	j, err := s.storage.GetJob(req.Context(), "job-3")
	require.NoError(t, err)
	require.Equal(t, "approved", j.PlanStatus)
	require.Equal(t, "RUNNING", j.Status)
	require.NotNil(t, j.PlanAcceptedAt)

	tasks, err := s.storage.GetJobTasks(req.Context(), "org-1", "job-3")
	require.NoError(t, err)
	require.Len(t, tasks, 2)
	var continuation *store.QueuedTask
	for i := range tasks {
		if tasks[i].Origin == store.OriginPlanApproved {
			continuation = &tasks[i]
		}
	}
	require.NotNil(t, continuation)
	require.NotNil(t, continuation.ParentTaskID)
	require.Equal(t, "task-3a", *continuation.ParentTaskID)
	require.Equal(t, "task-3a", continuation.RootTaskID)
	require.Equal(t, store.TaskQueued, continuation.Status)
	require.Equal(t, "claude-3-5-haiku", continuation.Spec["model"])
}

func TestHandleApproveJobPlanConflictWhenNotPendingReview(t *testing.T) {
	s, mux := newTestPlanServer(t)
	job := seedJobPendingReviewWithLeasedTask(t, s, "org-1", "job-not-pending", "task-np")
	// Approve once so plan_status is 'approved'
	require.NoError(t, s.storage.ApproveJobPlan(context.Background(), job.ID))

	body, _ := json.Marshal(map[string]string{"user_comment": "looks right"})
	req := authedRequest(t, http.MethodPost, "/api/v1/jobs/job-not-pending/plan/approve", bytes.NewReader(body), "org-1")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	require.Equal(t, http.StatusConflict, w.Code)
}

func TestHandleRejectJobPlan(t *testing.T) {
	s, mux := newTestPlanServer(t)
	seedJobPendingReview(t, s, "org-1", "job-4")

	body, _ := json.Marshal(map[string]string{"feedback": "wrong approach"})
	req := authedRequest(t, http.MethodPost, "/api/v1/jobs/job-4/plan/reject", bytes.NewReader(body), "org-1")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var resp struct {
		Status          string `json:"status"`
		PlannerNotified bool   `json:"planner_notified"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Equal(t, "rejected", resp.Status)
	require.True(t, resp.PlannerNotified)

	j, err := s.storage.GetJob(req.Context(), "job-4")
	require.NoError(t, err)
	require.Equal(t, "rejected", j.PlanStatus)
	require.Equal(t, "FAILED", j.Status)
	require.Equal(t, "wrong approach", j.PlanRejectedReason)
}

func TestHandleRejectJobPlanConflictWhenNotPendingReview(t *testing.T) {
	s, mux := newTestPlanServer(t)
	job := seedJobPendingReview(t, s, "org-1", "job-rejected-twice")
	require.NoError(t, s.storage.RejectJobPlan(context.Background(), job.ID, "first reason"))

	body, _ := json.Marshal(map[string]string{"feedback": "second reason"})
	req := authedRequest(t, http.MethodPost, "/api/v1/jobs/job-rejected-twice/plan/reject", bytes.NewReader(body), "org-1")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	require.Equal(t, http.StatusConflict, w.Code)
}

func TestHandlePlanMethodNotAllowed(t *testing.T) {
	s, mux := newTestPlanServer(t)
	seedJobPendingReview(t, s, "org-1", "job-method")

	// POST to /plan
	req := authedRequest(t, http.MethodPost, "/api/v1/jobs/job-method/plan", bytes.NewReader([]byte("{}")), "org-1")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	require.Equal(t, http.StatusMethodNotAllowed, w.Code)

	// GET to /plan/approve
	req = authedRequest(t, http.MethodGet, "/api/v1/jobs/job-method/plan/approve", nil, "org-1")
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	require.Equal(t, http.StatusMethodNotAllowed, w.Code)

	// GET to /plan/reject
	req = authedRequest(t, http.MethodGet, "/api/v1/jobs/job-method/plan/reject", nil, "org-1")
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	require.Equal(t, http.StatusMethodNotAllowed, w.Code)
}

func seedPlainJob(t *testing.T, s *Server, orgID, jobID string) *store.Job {
	t.Helper()
	job := &store.Job{
		ID:     jobID,
		OrgID:  orgID,
		UserID: "usr-1",
		Status: "QUEUED",
		Inputs: map[string]interface{}{"task": "test"},
	}
	require.NoError(t, s.storage.CreateJobWithOutbox(context.Background(), job, &store.Outbox{
		JobID: jobID, Topic: "job.created", Payload: map[string]interface{}{},
	}))
	return job
}

func TestHandleJobSpendCap(t *testing.T) {
	s, mux := newTestPlanServer(t)
	seedPlainJob(t, s, "org-1", "job-5")

	body, _ := json.Marshal(map[string]float64{"spend_cap_usd": 1.5})
	req := authedRequest(t, http.MethodPut, "/api/v1/jobs/job-5/spend-cap", bytes.NewReader(body), "org-1")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Equal(t, "job-5", resp["job_id"])
	require.Equal(t, 1.5, resp["spend_cap_usd"])

	j, err := s.storage.GetJob(req.Context(), "job-5")
	require.NoError(t, err)
	require.Equal(t, 1.5, j.SpendCapUSD)
}

func TestHandleJobSpendCapValidation(t *testing.T) {
	s, mux := newTestPlanServer(t)
	seedPlainJob(t, s, "org-1", "job-val")

	// Negative spend cap
	body, _ := json.Marshal(map[string]float64{"spend_cap_usd": -1.0})
	req := authedRequest(t, http.MethodPut, "/api/v1/jobs/job-val/spend-cap", bytes.NewReader(body), "org-1")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code)

	// Malformed JSON
	req = authedRequest(t, http.MethodPut, "/api/v1/jobs/job-val/spend-cap", bytes.NewReader([]byte("not json")), "org-1")
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	require.Equal(t, http.StatusBadRequest, w.Code)
}

func TestHandleJobSpendCapNotFoundOrOtherOrg(t *testing.T) {
	s, mux := newTestPlanServer(t)
	seedPlainJob(t, s, "org-1", "job-org1")

	// Non-existent job
	body, _ := json.Marshal(map[string]float64{"spend_cap_usd": 2.0})
	req := authedRequest(t, http.MethodPut, "/api/v1/jobs/nonexistent/spend-cap", bytes.NewReader(body), "org-1")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	require.Equal(t, http.StatusNotFound, w.Code)

	// Job belonging to other org
	req = authedRequest(t, http.MethodPut, "/api/v1/jobs/job-org1/spend-cap", bytes.NewReader(body), "org-2")
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	require.Equal(t, http.StatusNotFound, w.Code)
}

func TestHandleJobSpendCapMethodNotAllowed(t *testing.T) {
	s, mux := newTestPlanServer(t)
	seedPlainJob(t, s, "org-1", "job-meth")

	// GET
	req := authedRequest(t, http.MethodGet, "/api/v1/jobs/job-meth/spend-cap", nil, "org-1")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	require.Equal(t, http.StatusMethodNotAllowed, w.Code)

	// POST
	body, _ := json.Marshal(map[string]float64{"spend_cap_usd": 1.0})
	req = authedRequest(t, http.MethodPost, "/api/v1/jobs/job-meth/spend-cap", bytes.NewReader(body), "org-1")
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	require.Equal(t, http.StatusMethodNotAllowed, w.Code)
}
