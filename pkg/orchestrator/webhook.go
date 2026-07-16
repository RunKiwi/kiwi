package orchestrator

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
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

	// Fail closed: without a configured secret we cannot authenticate Linear,
	// and this endpoint spawns billable runs — reject rather than accept
	// unauthenticated requests.
	secret := os.Getenv("LINEAR_WEBHOOK_SECRET")
	if secret == "" {
		log.Println("[webhook] LINEAR_WEBHOOK_SECRET not set; rejecting webhook (fail closed)")
		http.Error(w, "Webhook not configured", http.StatusServiceUnavailable)
		return
	}

	// Bound the body to prevent memory-exhaustion DoS on this public endpoint.
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read body", http.StatusBadRequest)
		return
	}

	// Verify the HMAC-SHA256 signature over the raw body (constant-time).
	signature := r.Header.Get("Linear-Signature")
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	expectedMAC := hex.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(signature), []byte(expectedMAC)) {
		http.Error(w, "Invalid signature", http.StatusUnauthorized)
		return
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
		// A Linear retry of an already-processed delivery collides on the
		// per-org idempotency key — that's success (idempotent), not an error,
		// so return 200 to stop Linear's retry loop.
		var existing store.Job
		if qerr := s.storage.DB().WithContext(r.Context()).
			Where("org_id = ? AND idempotency_key = ?", orgID, idempotencyKey).
			First(&existing).Error; qerr == nil {
			w.WriteHeader(http.StatusOK)
			return
		}
		http.Error(w, "Failed to create job", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusOK)
}
