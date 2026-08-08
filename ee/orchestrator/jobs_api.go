// SPDX-License-Identifier: LicenseRef-Kiwi-BSL-1.1
// Copyright (c) 2026 RunKiwi. Licensed under the Business Source License 1.1.
// See ee/LICENSE. This is Control Plane code and is NOT Apache-2.0.

package orchestrator

import (
	"encoding/json"
	"log"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/ibreakthecloud/kiwi/ee/auth"
	"github.com/ibreakthecloud/kiwi/pkg/store"
	"github.com/ibreakthecloud/kiwi/pkg/ver"
)

type JobTaskResponse struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	// Task is this worker's own goal, from its spec — the drawer showed only
	// opaque ids like "job_1ec…-impl", which say nothing about what is running.
	Task         string  `json:"task,omitempty"`
	ResultURL    *string `json:"result_url,omitempty"`
	ResultDetail *string `json:"result_detail,omitempty"`

	// DependsOn lists the sibling worker ids this task waits on, as the planner
	// recorded them. Bare worker ids, not task ids: the caller knows the job id
	// and prefixing here would only make it strip the prefix back off.
	DependsOn []string `json:"depends_on,omitempty"`
	Model     string   `json:"model,omitempty"`
	Files     []string `json:"files,omitempty"`

	// Timing, so a caller can say "queued 4m" / "running 2m, attempt 2" rather
	// than showing an ageless spinner. StartedAt is set once at lease.
	QueuedAt  time.Time  `json:"queued_at"`
	StartedAt *time.Time `json:"started_at,omitempty"`
	// UpdatedAt is the last write to the row. On a TERMINAL task that is the
	// completion time, which is the only finish timestamp the schema has — there
	// is no completed_at. It is deliberately not a finish time on a RUNNING
	// task, because RenewLease bumps it every few minutes (the same reason
	// agent-minute metering reads StartedAt instead; see migration 0020).
	//
	// Exposed so a caller can report total elapsed. Without it the drawer could
	// only show the test-command duration, which reported two minutes for a task
	// that took thirteen — the missing eleven being an orphaned lease.
	UpdatedAt time.Time `json:"updated_at"`
	Attempts  int       `json:"attempts"`
	// LeasedBy is the daemon executing the task, when one holds the lease.
	LeasedBy *string `json:"leased_by,omitempty"`

	// BlockedReason is a stable code explaining why a QUEUED task has not
	// started (see store.Block*), and BlockedDetail the sentence to show. Both
	// are empty for tasks that are running or terminal. Without these a QUEUED
	// task is indistinguishable from a stuck one — the gap that let a job sit on
	// a spinner for 30 minutes while its org had no runner at all.
	BlockedReason string `json:"blocked_reason,omitempty"`
	BlockedDetail string `json:"blocked_detail,omitempty"`
}

type JobStatusResponse struct {
	JobID string `json:"job_id"`
	// Task is the overall goal that produced this job (the planner stamps it on
	// every worker spec as job_task), so the drawer can name what it is showing.
	Task  string            `json:"task,omitempty"`
	Repo  string            `json:"repo,omitempty"`
	Tasks []JobTaskResponse `json:"tasks"`
}

type JobsListResponse struct {
	Jobs []store.JobSummary `json:"jobs"`
}

func (s *Server) handleJobsList(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	claims := auth.ClaimsFromContext(r.Context())
	if claims == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	jobs, err := s.storage.ListJobs(r.Context(), claims.OrgID)
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	resp := JobsListResponse{
		Jobs: jobs,
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func (s *Server) handleJobStatus(w http.ResponseWriter, r *http.Request) {
	claims := auth.ClaimsFromContext(r.Context())
	if claims == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	// Lifecycle sub-routes are mounted under the job path rather than as separate
	// top-level handlers, so they inherit this handler's org scoping by
	// construction and cannot be reached without it.
	if action, ok := jobAction(r.URL.Path); ok {
		s.handleJobLifecycle(w, r, claims.OrgID, filepath.Base(filepath.Dir(r.URL.Path)), action)
		return
	}
	if r.Method == http.MethodDelete {
		s.handleJobLifecycle(w, r, claims.OrgID, filepath.Base(r.URL.Path), "delete")
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	jobID := filepath.Base(r.URL.Path)
	if strings.HasSuffix(r.URL.Path, "/record") {
		jobID = filepath.Base(filepath.Dir(r.URL.Path))
		s.handleJobRecord(w, r, claims.OrgID, jobID)
		return
	}
	if strings.HasSuffix(r.URL.Path, "/progress") {
		jobID = filepath.Base(filepath.Dir(r.URL.Path))
		s.handleJobProgress(w, r, claims.OrgID, jobID)
		return
	}

	tasks, err := s.storage.GetJobTasks(r.Context(), claims.OrgID, jobID)
	if err != nil {
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	if len(tasks) == 0 {
		http.Error(w, "Job not found", http.StatusNotFound)
		return
	}

	// Why any QUEUED task has not started. Best-effort: a diagnosis is an
	// explanation of the status, not the status itself, so failing to compute one
	// must not fail the status request that carries it.
	diagnoses, err := s.storage.DiagnoseQueuedTasks(r.Context(), claims.OrgID, tasks)
	if err != nil {
		log.Printf("[jobs] diagnose queued tasks for job %s: %v", jobID, err)
		diagnoses = nil
	}

	resp := JobStatusResponse{
		JobID: jobID,
		Tasks: make([]JobTaskResponse, len(tasks)),
	}

	for i, t := range tasks {
		var resultURL, resultDetail, leasedBy *string
		if t.ResultURL != nil {
			val := *t.ResultURL
			resultURL = &val
		}
		if t.ResultDetail != nil {
			val := *t.ResultDetail
			resultDetail = &val
		}
		if t.LeasedBy != nil {
			val := *t.LeasedBy
			leasedBy = &val
		}
		resp.Tasks[i] = JobTaskResponse{
			ID:           t.ID,
			Status:       t.Status,
			Task:         specString(t.Spec, "task"),
			ResultURL:    resultURL,
			ResultDetail: resultDetail,
			DependsOn:    specStrings(t.Spec, "depends_on"),
			Model:        specString(t.Spec, "model"),
			Files:        specStrings(t.Spec, "files"),
			QueuedAt:     t.CreatedAt,
			StartedAt:    t.StartedAt,
			UpdatedAt:    t.UpdatedAt,
			Attempts:     t.Attempts,
			LeasedBy:     leasedBy,
		}
		// The job-level goal and repo are stamped on every worker spec; take them
		// from the first task that carries them.
		if resp.Task == "" {
			resp.Task = specString(t.Spec, "job_task")
		}
		if resp.Repo == "" {
			resp.Repo = store.ShortRepo(specString(t.Spec, "repo_url"))
		}
		if d, ok := diagnoses[t.ID]; ok {
			resp.Tasks[i].BlockedReason = d.Reason
			resp.Tasks[i].BlockedDetail = d.Detail
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// handleJobRecord serves a job's execution record. The lookup is org-scoped, so
// another tenant's job ID is indistinguishable from one that does not exist —
// a 403 here would confirm the record's existence.
func (s *Server) handleJobRecord(w http.ResponseWriter, r *http.Request, orgID, jobID string) {
	rec, err := s.storage.GetExecutionRecord(r.Context(), orgID, jobID)
	if err != nil {
		http.Error(w, "Record not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	// The chain link, so a caller can verify continuity without re-canonicalizing.
	w.Header().Set("X-Kiwi-Record-Hash", rec.RecordHash)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(rec.Body)
}

// specStrings reads a []string out of a worker spec. The spec is jsonb, so the
// slice arrives as []interface{}; a malformed element is skipped rather than
// failing the whole response.
func specStrings(spec map[string]interface{}, key string) []string {
	if spec == nil {
		return nil
	}
	if v, ok := spec[key]; ok {
		if arr, ok := v.([]interface{}); ok {
			var res []string
			for _, item := range arr {
				if s, ok := item.(string); ok {
					res = append(res, s)
				}
			}
			return res
		}
	}
	return nil
}

// jobProgressTask is one worker's live state: the phases it has completed and
// what it is doing right now.
type jobProgressTask struct {
	TaskID string          `json:"task_id"`
	Status string          `json:"status"`
	Model  string          `json:"actor_model,omitempty"`
	Steps  []ver.TaskEvent `json:"steps"`
	Phase  string          `json:"phase,omitempty"`
	Output string          `json:"output_tail,omitempty"`
	// ProgressAt is when the daemon last reported. A timestamp that stops
	// advancing is how a hung run distinguishes itself from a slow one.
	ProgressAt *time.Time `json:"progress_at,omitempty"`
}

// handleJobProgress serves GET /api/v1/jobs/{id}/progress — what is happening
// right now, for a job that has not finished.
//
// Separate from /record deliberately. A record is a signed, hash-chained
// artifact assembled once at the end and never revised; this is a mutable view
// of a run in flight. Serving the two from one endpoint would either delay the
// record until it could be trusted or imply these partial rows carry the same
// weight, and neither is true.
func (s *Server) handleJobProgress(w http.ResponseWriter, r *http.Request, orgID, jobID string) {
	tasks, err := s.storage.GetJobTasks(r.Context(), orgID, jobID)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if len(tasks) == 0 {
		http.Error(w, "job not found", http.StatusNotFound)
		return
	}

	out := make([]jobProgressTask, 0, len(tasks))
	for _, t := range tasks {
		p := jobProgressTask{
			TaskID: t.ID,
			Status: t.Status,
			Model:  specString(t.Spec, "model"),
			Steps:  s.taskEventsFor(r.Context(), orgID, t.ID),
		}
		if t.ProgressPhase != nil {
			p.Phase = *t.ProgressPhase
		}
		if t.ProgressOutput != nil {
			p.Output = *t.ProgressOutput
		}
		p.ProgressAt = t.ProgressAt
		out = append(out, p)
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"tasks": out})
}
