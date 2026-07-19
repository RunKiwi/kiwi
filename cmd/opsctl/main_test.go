package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestProvisionDaemon(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/daemon/join-token" {
			t.Errorf("expected path /api/v1/daemon/join-token, got %s", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}

		var req struct {
			OrgID string `json:"org_id"`
		}
		json.NewDecoder(r.Body).Decode(&req)

		if req.OrgID != "test-org-123" {
			t.Errorf("expected org_id 'test-org-123', got '%s'", req.OrgID)
		}

		json.NewEncoder(w).Encode(map[string]string{"token": "fake-token-123"})
	}))
	defer ts.Close()

	args := []string{
		"-org-id", "test-org-123",
		"-api-url", ts.URL,
	}

	var out bytes.Buffer
	if err := run(args, &out); err != nil {
		t.Fatalf("run failed: %v", err)
	}

	result := out.String()
	if !strings.Contains(result, "org_id       = \"test-org-123\"") {
		t.Errorf("missing org_id in output")
	}
	if !strings.Contains(result, "join_token   = \"fake-token-123\"") {
		t.Errorf("missing join_token in output")
	}
}
