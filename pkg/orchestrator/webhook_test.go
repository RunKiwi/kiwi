package orchestrator

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ibreakthecloud/kiwi/pkg/store"
)

func signBody(secret, body string) string {
	m := hmac.New(sha256.New, []byte(secret))
	m.Write([]byte(body))
	return hex.EncodeToString(m.Sum(nil))
}

func newWebhookServer(t *testing.T) *Server {
	t.Helper()
	db := newTestDB(t)
	if err := db.AutoMigrate(&store.Job{}, &store.Outbox{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return &Server{db: db, storage: store.NewPostgresStore(db)}
}

func webhookReq(body, sig string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/linear/org-123", strings.NewReader(body))
	if sig != "" {
		req.Header.Set("Linear-Signature", sig)
	}
	return req
}

func TestWebhookFailsClosedWithoutSecret(t *testing.T) {
	t.Setenv("LINEAR_WEBHOOK_SECRET", "")
	s := newWebhookServer(t)
	rw := httptest.NewRecorder()
	s.handleLinearWebhook(rw, webhookReq(`{}`, ""))
	if rw.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 (fail closed) when no secret configured, got %d", rw.Code)
	}
}

func TestWebhookRejectsBadSignature(t *testing.T) {
	t.Setenv("LINEAR_WEBHOOK_SECRET", "shh")
	s := newWebhookServer(t)
	rw := httptest.NewRecorder()
	s.handleLinearWebhook(rw, webhookReq(`{"type":"Issue","action":"create"}`, "deadbeef"))
	if rw.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 for bad signature, got %d", rw.Code)
	}
}

func TestWebhookCreatesJobAndDedupesOnRetry(t *testing.T) {
	t.Setenv("LINEAR_WEBHOOK_SECRET", "shh")
	s := newWebhookServer(t)
	body := `{"type":"Issue","action":"create","data":{"id":"iss-1","title":"Fix","description":"d","labels":[{"name":"kiwi"}]}}`
	sig := signBody("shh", body)

	rw := httptest.NewRecorder()
	s.handleLinearWebhook(rw, webhookReq(body, sig))
	if rw.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rw.Code, rw.Body.String())
	}
	var count int64
	s.db.Model(&store.Job{}).Where("org_id = ?", "org-123").Count(&count)
	if count != 1 {
		t.Fatalf("expected 1 job created, got %d", count)
	}

	// Linear retries deliveries; the same issue id must dedupe to 200, not 500,
	// and must not create a second job.
	rw2 := httptest.NewRecorder()
	s.handleLinearWebhook(rw2, webhookReq(body, sig))
	if rw2.Code != http.StatusOK {
		t.Fatalf("retry expected 200 (idempotent), got %d: %s", rw2.Code, rw2.Body.String())
	}
	s.db.Model(&store.Job{}).Where("org_id = ?", "org-123").Count(&count)
	if count != 1 {
		t.Errorf("Linear retry must not create a duplicate job, got %d", count)
	}
}

func TestWebhookIgnoresNonTriggerIssues(t *testing.T) {
	t.Setenv("LINEAR_WEBHOOK_SECRET", "shh")
	s := newWebhookServer(t)
	body := `{"type":"Issue","action":"create","data":{"id":"iss-2","title":"x","state":{"name":"Backlog"}}}`
	sig := signBody("shh", body)
	rw := httptest.NewRecorder()
	s.handleLinearWebhook(rw, webhookReq(body, sig))
	if rw.Code != http.StatusOK {
		t.Fatalf("expected 200 no-op, got %d", rw.Code)
	}
	var count int64
	s.db.Model(&store.Job{}).Count(&count)
	if count != 0 {
		t.Errorf("issue without kiwi label / not In Progress must not create a job, got %d", count)
	}
}
