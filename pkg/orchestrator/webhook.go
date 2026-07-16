package orchestrator

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
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

	orgID := strings.TrimPrefix(r.URL.Path, "/api/v1/webhooks/linear/")
	if orgID == "" {
		http.Error(w, "Missing org ID in path", http.StatusBadRequest)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read body", http.StatusInternalServerError)
		return
	}

	secret := os.Getenv("LINEAR_WEBHOOK_SECRET")
	if secret != "" {
		signature := r.Header.Get("Linear-Signature")
		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write(body)
		expectedMAC := hex.EncodeToString(mac.Sum(nil))
		if !hmac.Equal([]byte(signature), []byte(expectedMAC)) {
			http.Error(w, "Invalid signature", http.StatusUnauthorized)
			return
		}
	}

	var payload LinearWebhookPayload
	if err := json.Unmarshal(body, &payload); err != nil {
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

	userID := "linear-webhook"
	taskID := generateTaskID()
	taskDesc := fmt.Sprintf("%s\n\n%s", payload.Data.Title, payload.Data.Description)
	idempotencyKey := "linear:" + payload.Data.Id

	job := &store.Job{
		ID:             taskID,
		OrgID:          orgID,
		UserID:         userID,
		Status:         "PENDING",
		IdempotencyKey: &idempotencyKey,
		Inputs: map[string]interface{}{
			"task":     taskDesc,
			"issue_id": payload.Data.Id,
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
