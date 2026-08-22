package daemon

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/ibreakthecloud/kiwi/pkg/agent"
	"github.com/ibreakthecloud/kiwi/pkg/sandbox"
	"github.com/ibreakthecloud/kiwi/pkg/session"
	"github.com/ibreakthecloud/kiwi/pkg/store"
)

func TestExecuteSessionReportsPlanReviewWithoutPublishing(t *testing.T) {
	specID := "session-task-plan-review"
	target := seedWorkspace(t, specID, "main.go", "package x\n")

	m := &sessionMock{specs: []session.Spec{
		{Verdict: session.VerdictProceed, Objective: "add a retry test"},
	}}
	d := newSessionTestDaemon(t, m)

	spec := agent.WorkerSpec{
		ID:                   specID,
		Model:                "sonnet",
		Task:                 "add a retry test",
		TestCmd:              "true",
		RequiresPlanApproval: true,
	}

	prog := &progressReporter{}
	deps := sessionDeps{
		leaseID:      "lease-review-1",
		worktreePath: filepath.Dir(target),
		sandboxCfg:   &sandbox.SandboxConfig{UseDocker: false},
		testCmd:      "true",
		verify:       func(ctx context.Context) (string, bool, error) { return "", true, nil },
	}

	result := d.executeSession(context.Background(), spec, map[string]string{"ANTHROPIC_API_KEY": "test-key"}, prog, deps)

	if result.ok {
		t.Fatal("expected result.ok == false when plan requires approval")
	}
	if result.planReviewStatus != store.TaskPlanReview {
		t.Fatalf("expected planReviewStatus %q, got %q", store.TaskPlanReview, result.planReviewStatus)
	}
	if result.planSpecJSON == "" {
		t.Fatal("expected non-empty planSpecJSON")
	}

	var specParsed session.Spec
	if err := json.Unmarshal([]byte(result.planSpecJSON), &specParsed); err != nil {
		t.Fatalf("failed to unmarshal planSpecJSON: %v", err)
	}
	if specParsed.Objective != "add a retry test" {
		t.Fatalf("expected Objective %q, got %q", "add a retry test", specParsed.Objective)
	}
	if result.prURL != "" {
		t.Fatalf("expected no prURL when plan review pending, got %q", result.prURL)
	}
}

func TestReportResult_PlanReview(t *testing.T) {
	var reportedReq ResultReq
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/daemon/result" {
			if err := json.NewDecoder(r.Body).Decode(&reportedReq); err != nil {
				t.Fatalf("decode ResultReq: %v", err)
			}
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	d, err := New(Config{APIURL: server.URL, CacheDir: t.TempDir()})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := d.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	out := taskResult{
		ok:               false,
		planReviewStatus: store.TaskPlanReview,
		planSpecJSON:     `{"objective":"test plan"}`,
		detail:           "plan pending review",
	}
	d.reportResult(context.Background(), "task-1", "lease-1", out)

	if reportedReq.Status != store.TaskPlanReview {
		t.Fatalf("expected status %q, got %q", store.TaskPlanReview, reportedReq.Status)
	}
	if reportedReq.PlanSpecJSON != `{"objective":"test plan"}` {
		t.Fatalf("expected PlanSpecJSON %q, got %q", `{"objective":"test plan"}`, reportedReq.PlanSpecJSON)
	}
	if reportedReq.Detail != "plan pending review" {
		t.Fatalf("expected Detail %q, got %q", "plan pending review", reportedReq.Detail)
	}
}
