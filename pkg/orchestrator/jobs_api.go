package orchestrator

import (
	"encoding/json"
	"log"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/ibreakthecloud/kiwi/pkg/auth"
	"github.com/ibreakthecloud/kiwi/pkg/store"
)

type JobTaskResponse struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	// Task is this worker's own goal, from its spec — the drawer showed only
	// opaque ids like "job_1ec…-impl", which say nothing about what is running.
	Task         string  `json:"task,omitempty"`
	ResultURL    *string `json:"result_url,omitempty"`
	ResultDetail *string `json:"result_detail,omitempty"`

	// Timing, so a caller can say "queued 4m" / "running 2m, attempt 2" rather
	// than showing an ageless spinner. StartedAt is set once at lease.
	QueuedAt  time.Time  `json:"queued_at"`
	StartedAt *time.Time `json:"started_at,omitempty"`
	Attempts  int        `json:"attempts"`
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
			QueuedAt:     t.CreatedAt,
			StartedAt:    t.StartedAt,
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
