package daemon

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestTelemetryDueDecodesDuePolls(t *testing.T) {
	_, priv, err := crypto.GenerateSigningKeyPair()
	if err != nil {
		t.Fatalf("failed to generate signing key: %v", err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/daemon/telemetry/due" {
			t.Errorf("path = %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"due":[{"poll_id":"poll_1","provider":"datadog","query":"q","baseline_start":"2026-08-15T00:00:00Z","baseline_end":"2026-08-15T01:00:00Z","current_start":"2026-08-17T00:00:00Z","current_end":"2026-08-17T00:15:00Z"}]}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	c.SetSigner(priv)

	res, err := c.TelemetryDue(context.Background(), TelemetryDueReq{SignPubKey: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Due) != 1 || res.Due[0].PollID != "poll_1" {
		t.Fatalf("got %+v", res.Due)
	}
}

func TestTelemetryDueNoContentReturnsEmpty(t *testing.T) {
	_, priv, err := crypto.GenerateSigningKeyPair()
	if err != nil {
		t.Fatalf("failed to generate signing key: %v", err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	c.SetSigner(priv)

	res, err := c.TelemetryDue(context.Background(), TelemetryDueReq{SignPubKey: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if res != nil && len(res.Due) != 0 {
		t.Errorf("got %+v, want empty/nil on 204", res)
	}
}

func TestTelemetryReportPostsResults(t *testing.T) {
	_, priv, err := crypto.GenerateSigningKeyPair()
	if err != nil {
		t.Fatalf("failed to generate signing key: %v", err)
	}

	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	c.SetSigner(priv)

	err = c.TelemetryReport(context.Background(), TelemetryReportReq{
		SignPubKey: "test",
		Results: []TelemetryPollResult{
			{PollID: "poll_1", Baseline: &TelemetryResultDTO{SampleCount: 40, Mean: 100}, Current: &TelemetryResultDTO{SampleCount: 40, Mean: 105}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(gotBody), "poll_1") {
		t.Errorf("body = %s, want it to contain poll_1", gotBody)
	}
}
