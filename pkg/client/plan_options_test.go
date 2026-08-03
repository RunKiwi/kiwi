package client

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// capturePlan returns the JSON body the client sends for a given PlanOptions.
func capturePlan(t *testing.T, opts PlanOptions) map[string]any {
	t.Helper()
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"job_id":"job_1"}`))
	}))
	defer srv.Close()

	c := New(srv.URL, "tok")
	if _, err := c.PlanTask(context.Background(), opts); err != nil {
		t.Fatalf("PlanTask: %v", err)
	}
	return got
}

// The default path must be byte-identical to what it was before mode existed.
// Sending "mode":"" would make every ordinary submission look like it had an
// opinion about a field the caller never set, and the planner's own default is
// the absent key.
func TestPlanOmitsModeWhenUnset(t *testing.T) {
	body := capturePlan(t, PlanOptions{
		Task: "fix it", RepoURL: "https://github.com/x/y", Ref: "main",
		File: "a.go", TestCmd: "go test ./...", MaxWorkers: 1,
	})

	if _, present := body["mode"]; present {
		t.Errorf("mode was sent on a default submission: %v", body["mode"])
	}
	if _, present := body["architect_model"]; present {
		t.Errorf("architect_model was sent when unset: %v", body["architect_model"])
	}
}

func TestPlanSendsSessionMode(t *testing.T) {
	body := capturePlan(t, PlanOptions{
		Task: "add a thing", RepoURL: "https://github.com/x/y", Ref: "main",
		TestCmd: "go test ./...", Model: "claude-haiku-4-5-20251001",
		Mode: "session", ArchitectModel: "claude-sonnet-5",
	})

	if body["mode"] != "session" {
		t.Errorf("mode = %v, want session", body["mode"])
	}
	if body["architect_model"] != "claude-sonnet-5" {
		t.Errorf("architect_model = %v, want claude-sonnet-5", body["architect_model"])
	}
	// The worker model must still travel separately — the whole point of the
	// split is that the two differ.
	if body["model"] != "claude-haiku-4-5-20251001" {
		t.Errorf("model = %v, want the worker model unchanged", body["model"])
	}
}

// Session mode without an explicit architect is legal: the daemon falls back to
// Model. The key must simply be absent rather than sent empty.
func TestPlanSessionWithoutArchitectOmitsTheKey(t *testing.T) {
	body := capturePlan(t, PlanOptions{
		Task: "x", RepoURL: "https://github.com/x/y", Mode: "session",
	})
	if body["mode"] != "session" {
		t.Errorf("mode = %v, want session", body["mode"])
	}
	if _, present := body["architect_model"]; present {
		t.Error("architect_model must be omitted, not sent empty, so the daemon applies its fallback")
	}
}
