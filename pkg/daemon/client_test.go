package daemon

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ibreakthecloud/kiwi/pkg/agent"
	"github.com/ibreakthecloud/kiwi/pkg/crypto"
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

		var reqBody HeartbeatReq
		if err := json.NewDecoder(r.Body).Decode(&reqBody); err != nil {
			t.Fatalf("failed to decode request body: %v", err)
		}
		if reqBody.PubKey != "mock-pub-key" {
			t.Errorf("expected PubKey 'mock-pub-key', got '%s'", reqBody.PubKey)
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

	if res == nil {
		t.Fatalf("expected 1 result, got nil")
	}

	if len(res.Specs) == 0 {
		t.Fatalf("expected specs, got 0")
	}

	if res.Specs[0].ID != mockSpec.ID {
		t.Errorf("expected task ID %s, got %s", mockSpec.ID, res.Specs[0].ID)
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

func TestClient_Heartbeat_SignsRequest(t *testing.T) {
	pub, priv, err := crypto.GenerateSigningKeyPair()
	if err != nil {
		t.Fatalf("failed to generate signing key: %v", err)
	}

	var gotBody []byte
	var gotSig string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		gotSig = r.Header.Get("X-Kiwi-Signature")
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client := NewClient(server.URL)
	client.SetSigner(priv)

	_, err = client.Heartbeat(context.Background(), HeartbeatReq{PubKey: "k", SignPubKey: "s", Timestamp: 123})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if gotSig == "" {
		t.Fatal("expected X-Kiwi-Signature header, got none")
	}
	sig, err := base64.StdEncoding.DecodeString(gotSig)
	if err != nil {
		t.Fatalf("failed to decode signature: %v", err)
	}
	if !crypto.Verify(pub, gotBody, sig) {
		t.Error("signature did not verify against the request body")
	}
}

func TestClient_Heartbeat_UnsignedWhenNoSigner(t *testing.T) {
	var gotSig string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotSig = r.Header.Get("X-Kiwi-Signature")
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client := NewClient(server.URL)
	if _, err := client.Heartbeat(context.Background(), HeartbeatReq{PubKey: "k"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotSig != "" {
		t.Errorf("expected no signature header without a signer, got %q", gotSig)
	}
}

func TestClient_Heartbeat_DecodeError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("{ invalid json "))
	}))
	defer server.Close()

	client := NewClient(server.URL)
	ctx := context.Background()

	_, err := client.Heartbeat(ctx, HeartbeatReq{PubKey: "mock"})
	if err == nil {
		t.Fatal("expected error on malformed JSON response, got nil")
	}
}
