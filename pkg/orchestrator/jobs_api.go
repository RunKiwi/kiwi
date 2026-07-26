package orchestrator

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/ibreakthecloud/kiwi/pkg/auth"
	"github.com/ibreakthecloud/kiwi/pkg/store"
)

type JobTaskResponse struct {
	ID           string  `json:"id"`
	Status       string  `json:"status"`
	ResultURL    *string `json:"result_url,omitempty"`
	ResultDetail *string `json:"result_detail,omitempty"`
}

type JobStatusResponse struct {
	JobID string            `json:"job_id"`
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
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	claims := auth.ClaimsFromContext(r.Context())
	if claims == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
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

	resp := JobStatusResponse{
		JobID: jobID,
		Tasks: make([]JobTaskResponse, len(tasks)),
	}

	for i, t := range tasks {
		var resultURL, resultDetail *string
		if t.ResultURL != nil {
			val := *t.ResultURL
			resultURL = &val
		}
		if t.ResultDetail != nil {
			val := *t.ResultDetail
			resultDetail = &val
		}
		resp.Tasks[i] = JobTaskResponse{
			ID:           t.ID,
			Status:       t.Status,
			ResultURL:    resultURL,
			ResultDetail: resultDetail,
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
