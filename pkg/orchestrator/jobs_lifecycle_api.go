package orchestrator

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"
)

// jobAction recognises the lifecycle sub-routes of /api/v1/jobs/{id}. It returns
// the action and whether the path is one. "record" is deliberately excluded — it
// is a read served by the status handler, not a lifecycle mutation.
func jobAction(path string) (string, bool) {
	for _, a := range []string{"cancel", "retry"} {
		if strings.HasSuffix(path, "/"+a) {
			return a, true
		}
	}
	return "", false
}

// JobLifecycleResponse reports what a lifecycle action actually did. The count
// matters: "cancel" on a job whose tasks all finished a second earlier is a
// success that changed nothing, and the caller should be able to tell.
type JobLifecycleResponse struct {
	JobID   string `json:"job_id"`
	Action  string `json:"action"`
	Tasks   int    `json:"tasks_affected"`
	Message string `json:"message,omitempty"`
}

// handleJobLifecycle serves the mutating job routes:
//
//	POST   /api/v1/jobs/{id}/cancel   stop a running or queued job
//	POST   /api/v1/jobs/{id}/retry    requeue its failed/cancelled tasks
//	DELETE /api/v1/jobs/{id}          remove it from the dashboard
//
// orgID comes from the authenticated claims, never the path, so one tenant can
// neither mutate nor probe another's jobs: an unknown or foreign job id is an
// affected-count of zero, not a 403 that would confirm the id exists.
func (s *Server) handleJobLifecycle(w http.ResponseWriter, r *http.Request, orgID, jobID, action string) {
	if jobID == "" || jobID == "jobs" {
		http.Error(w, "job id is required", http.StatusBadRequest)
		return
	}
	if action != "delete" && r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var (
		n   int
		err error
		msg string
	)
	switch action {
	case "cancel":
		n, err = s.storage.CancelJob(r.Context(), orgID, jobID, "cancelled by user")
		// A LEASED task's daemon is a separate process; it learns of the
		// cancellation on its next lease renewal and aborts then. Say so, rather
		// than implying the compute stopped the instant the request returned.
		msg = "cancelled; any running task stops at its next lease renewal"
	case "retry":
		n, err = s.storage.RetryJob(r.Context(), orgID, jobID)
		msg = "requeued failed and cancelled tasks"
		if n == 0 {
			msg = "nothing to retry — only failed or cancelled tasks are requeued"
		}
	case "delete":
		// Cancel first, unconditionally. Deleting a LEASED task's row would strand
		// its daemon working against a task that no longer exists, and its result
		// would land nowhere. Cancelling revokes the lease so the daemon aborts.
		if _, cerr := s.storage.CancelJob(r.Context(), orgID, jobID, "job deleted by user"); cerr != nil {
			log.Printf("[jobs] cancel before delete %s: %v", jobID, cerr)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
			return
		}
		n, err = s.storage.DeleteJob(r.Context(), orgID, jobID)
		msg = "deleted; the job's execution record is retained to keep the provenance chain verifiable"
	default:
		http.Error(w, "unknown action", http.StatusBadRequest)
		return
	}

	if err != nil {
		log.Printf("[jobs] %s job %s: %v", action, jobID, err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(JobLifecycleResponse{
		JobID:   jobID,
		Action:  action,
		Tasks:   n,
		Message: msg,
	})
}
