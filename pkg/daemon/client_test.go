package daemon

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ibreakthecloud/kiwi/pkg/agent"
)

func TestClient_Heartbeat_OK(t *testing.T) {
	mockSpec := agent.WorkerSpec{
		ID:    "job-1-w0",
		Model: "sonnet",
		Task:  "test task",
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/daemon/heartbeat" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Fatalf("unexpected method: %s", r.Method)
		}

		res := HeartbeatRes{
			Specs: []agent.WorkerSpec{mockSpec},
		}
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(res)
	}))
	defer server.Close()

	client := NewClient(server.URL)
	ctx := context.Background()
	req := HeartbeatReq{PubKey: "mock-pub-key"}

	res, err := client.Heartbeat(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(res) == 0 {
		t.Fatalf("expected 1 result, got 0")
	}

	if res[0].Specs[0].ID != mockSpec.ID {
		t.Errorf("expected task ID %s, got %s", mockSpec.ID, res[0].Specs[0].ID)
	}
}

func TestClient_Heartbeat_NoContent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client := NewClient(server.URL)
	ctx := context.Background()

	res, err := client.Heartbeat(ctx, HeartbeatReq{PubKey: "mock"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if res != nil {
		t.Fatalf("expected nil response for 204 No Content, got %v", res)
	}
}

func TestClient_Heartbeat_Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("internal error"))
	}))
	defer server.Close()

	client := NewClient(server.URL)
	ctx := context.Background()

	_, err := client.Heartbeat(ctx, HeartbeatReq{PubKey: "mock"})
	if err == nil {
		t.Fatal("expected error on 500 response, got nil")
	}
}
