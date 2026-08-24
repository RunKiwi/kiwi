// SPDX-License-Identifier: LicenseRef-Kiwi-BSL-1.1
// Copyright (c) 2026 RunKiwi. Licensed under the Business Source License 1.1.
// See ee/LICENSE. This is Control Plane code and is NOT Apache-2.0.

package orchestrator

import (
	"encoding/json"
	"net/http"

	"github.com/ibreakthecloud/kiwi/ee/auth"
	"github.com/ibreakthecloud/kiwi/pkg/store"
)

// handleJobPlan serves GET /api/v1/jobs/{id}/plan,
// POST /api/v1/jobs/{id}/plan/approve, and POST /api/v1/jobs/{id}/plan/reject.
// Registered at the "/api/v1/jobs/" prefix already owned by handleJobStatus;
// dispatch here on the "/plan" suffix before falling through.
func (s *Server) handleJobPlan(w http.ResponseWriter, r *http.Request, jobID, action string) {
	claims := auth.ClaimsFromContext(r.Context())
	if claims == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	job, err := s.storage.GetJob(r.Context(), jobID)
	if err != nil {
		http.Error(w, "Job not found", http.StatusNotFound)
		return
	}
	if job.OrgID != claims.OrgID {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}

	switch action {
	case "":
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"job_id":            job.ID,
			"plan_status":       job.PlanStatus,
			"plan_markdown":     job.PlanMarkdown,
			"requires_approval": job.RequiresPlanApproval,
			"architect_model":   job.ArchitectModel,
			"created_at":        job.CreatedAt,
		})
	case "approve":
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		s.handleApproveJobPlan(w, r, job)
	case "reject":
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}
		s.handleRejectJobPlan(w, r, job)
	default:
		http.Error(w, "Not found", http.StatusNotFound)
	}
}

func (s *Server) handleApproveJobPlan(w http.ResponseWriter, r *http.Request, job *store.Job) {
	if job.PlanStatus != "pending_review" {
		http.Error(w, "plan is not pending review", http.StatusConflict)
		return
	}

	tasks, err := s.storage.GetJobTasks(r.Context(), job.OrgID, job.ID)
	if err != nil || len(tasks) == 0 {
		http.Error(w, "could not locate the paused task", http.StatusInternalServerError)
		return
	}
	// The task the daemon reported PLAN_REVIEW on is the most recent one on
	// the job's root thread — the same lookup pattern used to find the active
	// task in a PR-comment continuation (see ActiveTaskInThread).
	var parent *store.QueuedTask
	for i := range tasks {
		if tasks[i].Status == store.TaskPlanReview {
			parent = &tasks[i]
		}
	}
	if parent == nil {
		http.Error(w, "no plan-review task found for this job", http.StatusConflict)
		return
	}

	rootTaskID := parent.RootTaskID
	if rootTaskID == "" {
		rootTaskID = parent.ID
	}

	continuation := &store.QueuedTask{
		ID:           generateTaskID(),
		OrgID:        job.OrgID,
		JobID:        job.ID,
		ParentTaskID: &parent.ID,
		RootTaskID:   rootTaskID,
		Origin:       store.OriginPlanApproved,
		Status:       store.TaskQueued,
		Spec:         parent.Spec, // same worker-spec: same repo, model, test command, SessionID
		FleetID:      parent.FleetID,
	}
	if err := s.storage.ApproveJobPlanAndEnqueue(r.Context(), job.ID, continuation); err != nil {
		if err == store.ErrPlanStatusConflict {
			http.Error(w, "plan approval conflict: plan is not in pending_review state or was already approved", http.StatusConflict)
			return
		}
		http.Error(w, "failed to approve plan and enqueue continuation", http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"status": "approved", "resumed_phase": "actor"})
}

func (s *Server) handleRejectJobPlan(w http.ResponseWriter, r *http.Request, job *store.Job) {
	if job.PlanStatus != "pending_review" {
		http.Error(w, "plan is not pending review", http.StatusConflict)
		return
	}
	var body struct {
		Feedback string `json:"feedback"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)

	if len(body.Feedback) > 255 {
		http.Error(w, "feedback exceeds 255 character limit", http.StatusBadRequest)
		return
	}

	if err := s.storage.RejectJobPlan(r.Context(), job.ID, body.Feedback); err != nil {
		if err == store.ErrPlanStatusConflict {
			http.Error(w, "plan rejection conflict: plan is not in pending_review state", http.StatusConflict)
			return
		}
		http.Error(w, "failed to reject plan", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"status": "rejected", "planner_notified": true})
}

func (s *Server) handleJobSpendCap(w http.ResponseWriter, r *http.Request, orgID, jobID string) {
	if r.Method != http.MethodPut {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		SpendCapUSD float64 `json:"spend_cap_usd"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.SpendCapUSD < 0 {
		http.Error(w, "Bad request: 'spend_cap_usd' must be a non-negative number", http.StatusBadRequest)
		return
	}
	if err := s.storage.SetJobSpendCap(r.Context(), orgID, jobID, body.SpendCapUSD); err != nil {
		if err == store.ErrJobNotFound {
			http.Error(w, "Job not found", http.StatusNotFound)
			return
		}
		http.Error(w, "failed to set spend cap", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"job_id": jobID, "spend_cap_usd": body.SpendCapUSD})
}
