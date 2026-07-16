package orchestrator

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/ibreakthecloud/kiwi/pkg/store"
)

type LinearWebhookPayload struct {
	Action string `json:"action"`
	Type   string `json:"type"`
	Data   struct {
		Id          string `json:"id"`
		Title       string `json:"title"`
		Description string `json:"description"`
		State       struct {
			Name string `json:"name"`
		} `json:"state"`
		Labels []struct {
			Name string `json:"name"`
		} `json:"labels"`
	} `json:"data"`
}

func (s *Server) handleLinearWebhook(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var payload LinearWebhookPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		http.Error(w, "Invalid payload", http.StatusBadRequest)
		return
	}

	if payload.Type != "Issue" || (payload.Action != "create" && payload.Action != "update") {
		w.WriteHeader(http.StatusOK)
		return
	}

	isKiwi := false
	for _, l := range payload.Data.Labels {
		if strings.ToLower(l.Name) == "kiwi" {
			isKiwi = true
			break
		}
	}

	if !isKiwi && payload.Data.State.Name != "In Progress" {
		w.WriteHeader(http.StatusOK)
		return
	}

	orgID := "default-org"
	userID := "linear-webhook"

	taskID := generateTaskID()
	taskDesc := fmt.Sprintf("%s\n\n%s", payload.Data.Title, payload.Data.Description)

	job := &store.Job{
		ID:     taskID,
		OrgID:  orgID,
		UserID: userID,
		Status: "PENDING",
		Inputs: map[string]interface{}{
			"task": taskDesc,
		},
	}

	outbox := &store.Outbox{
		JobID: taskID,
		Topic: "jobs.submitted",
		Payload: map[string]interface{}{
			"job_id": taskID,
		},
	}

	if err := s.storage.CreateJobWithOutbox(r.Context(), job, outbox); err != nil {
		http.Error(w, "Failed to create job", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}
