package planner

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"github.com/ibreakthecloud/kiwi/pkg/auth"
	"github.com/ibreakthecloud/kiwi/pkg/store"
)

func newTestStore(t *testing.T) *store.PostgresStore {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&store.Manifest{}, &store.QueuedTask{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return store.NewPostgresStore(db)
}

func TestHeuristicPlannerDAG(t *testing.T) {
	p := NewHeuristicPlanner()
	plan, err := p.Plan(context.Background(), PlanRequest{Task: "fix the flaky test", MaxWorkers: 2})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	// analyze + 2 impl + verify = 4
	if len(plan.Workers) != 4 {
		t.Fatalf("expected 4 workers, got %d", len(plan.Workers))
	}
	byID := map[string]PlannedWorker{}
	for _, w := range plan.Workers {
		byID[w.ID] = w
	}
	if len(byID["analyze"].DependsOn) != 0 {
		t.Error("analyze should have no deps")
	}
	if got := byID["impl-0"].DependsOn; len(got) != 1 || got[0] != "analyze" {
		t.Errorf("impl-0 should depend on analyze, got %v", got)
	}
	if got := byID["verify"].DependsOn; len(got) != 2 {
		t.Errorf("verify should depend on both impl workers, got %v", got)
	}
}

func TestHeuristicPlannerRequiresTask(t *testing.T) {
	if _, err := NewHeuristicPlanner().Plan(context.Background(), PlanRequest{}); err == nil {
		t.Error("expected error for empty task")
	}
}

type fakeCompleter struct{ out string }

func (f fakeCompleter) Complete(ctx context.Context, system, user string) (string, error) {
	return f.out, nil
}

func TestLLMPlannerParsesFencedJSON(t *testing.T) {
	resp := "Sure, here is the plan:\n```json\n" +
		`{"summary":"one step","workers":[{"id":"w1","task":"do it","model":"sonnet","depends_on":[]}]}` +
		"\n```\n"
	p := NewLLMPlanner(fakeCompleter{out: resp})
	plan, err := p.Plan(context.Background(), PlanRequest{Task: "x"})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if len(plan.Workers) != 1 || plan.Workers[0].ID != "w1" {
		t.Errorf("unexpected plan: %+v", plan)
	}
}

func TestLLMPlannerRejectsGarbage(t *testing.T) {
	p := NewLLMPlanner(fakeCompleter{out: "not json at all"})
	if _, err := p.Plan(context.Background(), PlanRequest{Task: "x"}); err == nil {
		t.Error("expected error for non-JSON model output")
	}
}

func TestServiceSubmitPlanPersistsAndEnqueues(t *testing.T) {
	s := newTestStore(t)
	svc := NewService(s, NewHeuristicPlanner())
	ctx := context.Background()

	res, err := svc.SubmitPlan(ctx, PlanRequest{
		OrgID: "o1", Task: "fix bug", RepoURL: "https://github.com/x/y", Ref: "main", MaxWorkers: 2,
	})
	if err != nil {
		t.Fatalf("SubmitPlan: %v", err)
	}
	if res.ManifestID == "" {
		t.Error("expected a content-addressed manifest id")
	}
	if len(res.TaskIDs) != 4 {
		t.Fatalf("expected 4 enqueued tasks, got %d", len(res.TaskIDs))
	}

	// Manifest was persisted.
	var m store.Manifest
	if err := s.DB().First(&m, "id = ?", res.ManifestID).Error; err != nil {
		t.Fatalf("manifest not persisted: %v", err)
	}
	if m.Producer != "planner" || m.OrgID != "o1" {
		t.Errorf("unexpected manifest: %+v", m)
	}

	// Tasks are leasable from the queue for that org.
	leased, err := s.LeaseNextTask(ctx, "o1", "daemon-1", 60_000_000_000) // 60s
	if err != nil || leased == nil {
		t.Fatalf("expected a leasable task, got %v err=%v", leased, err)
	}
	if leased.Spec["repo_url"] != "https://github.com/x/y" {
		t.Errorf("task spec missing repo_url: %+v", leased.Spec)
	}
}

func TestHandlePlan(t *testing.T) {
	s := newTestStore(t)
	svc := NewService(s, NewHeuristicPlanner())

	body := `{"task":"add logging","repo_url":"https://github.com/x/y","ref":"main","max_workers":1}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/planner/plan", strings.NewReader(body))
	req = req.WithContext(auth.ContextWithClaims(req.Context(), &auth.UserClaims{OrgID: "o1", UserID: "u1"}))
	rec := httptest.NewRecorder()

	svc.HandlePlan(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", rec.Code, rec.Body.String())
	}
	var out SubmitResult
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if out.ManifestID == "" || len(out.TaskIDs) == 0 {
		t.Errorf("unexpected response: %+v", out)
	}
}

func TestHandlePlanRejectsMissingClaims(t *testing.T) {
	svc := NewService(newTestStore(t), NewHeuristicPlanner())
	req := httptest.NewRequest(http.MethodPost, "/api/v1/planner/plan", strings.NewReader(`{"task":"x"}`))
	rec := httptest.NewRecorder()
	svc.HandlePlan(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 without claims, got %d", rec.Code)
	}
}

func TestHandlePlanRejectsEmptyTask(t *testing.T) {
	svc := NewService(newTestStore(t), NewHeuristicPlanner())
	req := httptest.NewRequest(http.MethodPost, "/api/v1/planner/plan", strings.NewReader(`{"task":""}`))
	req = req.WithContext(auth.ContextWithClaims(req.Context(), &auth.UserClaims{OrgID: "o1"}))
	rec := httptest.NewRecorder()
	svc.HandlePlan(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for empty task, got %d", rec.Code)
	}
}
