package planner

import (
	"context"
	"errors"
	"testing"

	"github.com/ibreakthecloud/kiwi/pkg/store"
)

// capturePlanner records the request it was handed (so tests can assert what
// learnings SubmitPlan resolved) and returns a minimal executable plan.
type capturePlanner struct{ last PlanRequest }

func (c *capturePlanner) Plan(ctx context.Context, req PlanRequest) (*Plan, error) {
	c.last = req
	return &Plan{
		Summary: "plan summary",
		Workers: []PlannedWorker{{ID: "w1", Task: "t", File: "f", Model: "m", TestCmd: "go test"}},
	}, nil
}

// countingEmbedder counts Embed calls so we can prove auto mode embeds once.
type countingEmbedder struct{ n int }

func (e *countingEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	e.n++
	return make([]float32, 768), nil
}

type failingEmbedder struct{}

func (failingEmbedder) Embed(ctx context.Context, text string) ([]float32, error) {
	return nil, errors.New("embedder down")
}

func seedLearning(t *testing.T, s store.Store, jobID, orgID string) {
	t.Helper()
	if err := s.UpsertJobLearning(context.Background(), &store.JobLearning{
		ID: jobID, JobID: jobID, OrgID: orgID, Repo: "owner/repo",
		Task: "prior " + jobID, Summary: "did " + jobID,
	}); err != nil {
		t.Fatalf("seed %s: %v", jobID, err)
	}
}

// Indexing writes exactly one learning row for the submitted job, with the repo
// derived via the shared store.ShortRepo and a nil embedding when no embedder is
// configured — and a failing embedder never fails the submission.
func TestSubmitPlanIndexesLearning(t *testing.T) {
	s := newTestStore(t)
	svc := NewService(s, &capturePlanner{}, failingEmbedder{})
	svc.indexSync = true

	res, err := svc.SubmitPlan(context.Background(), PlanRequest{
		OrgID:   "org1",
		Task:    "add a feature",
		RepoURL: "https://github.com/owner/repo.git",
	})
	if err != nil {
		t.Fatalf("SubmitPlan (failing embedder must not fail submit): %v", err)
	}

	learnings, err := s.GetJobLearnings(context.Background(), "org1", []string{res.JobID})
	if err != nil {
		t.Fatalf("GetJobLearnings: %v", err)
	}
	if len(learnings) != 1 {
		t.Fatalf("expected 1 learning row, got %d", len(learnings))
	}
	if learnings[0].Repo != "owner/repo" {
		t.Errorf("repo = %q, want owner/repo", learnings[0].Repo)
	}
	if learnings[0].Embedding != nil {
		t.Errorf("expected nil embedding when embedder fails, got %v", learnings[0].Embedding)
	}
	if learnings[0].Summary != "plan summary" {
		t.Errorf("summary = %q, want the plan summary", learnings[0].Summary)
	}
}

// CompleteTask records the job's terminal outcome and PR on its learning row.
func TestSubmitPlanCompletionRecordsOutcome(t *testing.T) {
	s := newTestStore(t)
	svc := NewService(s, &capturePlanner{}, nil)
	svc.indexSync = true
	ctx := context.Background()

	res, err := svc.SubmitPlan(ctx, PlanRequest{OrgID: "org1", Task: "x", RepoURL: "https://github.com/owner/repo"})
	if err != nil {
		t.Fatalf("SubmitPlan: %v", err)
	}

	taskID := res.TaskIDs[0]
	leaseID := "lease1"
	s.DB().Model(&store.QueuedTask{}).Where("id = ?", taskID).
		Updates(map[string]interface{}{"status": store.TaskLeased, "lease_id": leaseID})

	pr := "https://github.com/owner/repo/pull/1"
	if ok, err := s.CompleteTask(ctx, taskID, leaseID, store.TaskSucceeded, pr, "done"); err != nil || !ok {
		t.Fatalf("CompleteTask: ok=%v err=%v", ok, err)
	}

	learnings, _ := s.GetJobLearnings(ctx, "org1", []string{res.JobID})
	if len(learnings) != 1 || learnings[0].Outcome == nil || *learnings[0].Outcome != "succeeded" {
		t.Fatalf("outcome not recorded as succeeded: %+v", learnings)
	}
	if learnings[0].PRURL == nil || *learnings[0].PRURL != pr {
		t.Fatalf("pr_url not recorded: %+v", learnings[0].PRURL)
	}
}

// Manual reference resolves only the caller's own jobs — never another tenant's.
func TestSubmitPlanManualResolutionIsOrgScoped(t *testing.T) {
	s := newTestStore(t)
	seedLearning(t, s, "jobA", "org1")
	seedLearning(t, s, "jobB", "org1")
	seedLearning(t, s, "jobC", "org2") // a different tenant's job

	cp := &capturePlanner{}
	svc := NewService(s, cp, nil)
	svc.indexSync = true

	_, err := svc.SubmitPlan(context.Background(), PlanRequest{
		OrgID:           "org1",
		Task:            "new work",
		RepoURL:         "https://github.com/owner/repo",
		ReferenceMode:   "manual",
		ReferenceJobIDs: []string{"jobA", "jobB", "jobC"},
	})
	if err != nil {
		t.Fatalf("SubmitPlan: %v", err)
	}
	if got := len(cp.last.ResolvedLearnings); got != 2 {
		t.Fatalf("expected 2 same-org learnings resolved, got %d", got)
	}
	for _, l := range cp.last.ResolvedLearnings {
		if l.OrgID != "org1" {
			t.Errorf("resolved a foreign-tenant learning: %+v", l)
		}
	}
}

// Manual reference is capped at three learnings even when more are selected.
func TestSubmitPlanManualCapsAtThree(t *testing.T) {
	s := newTestStore(t)
	ids := []string{"j1", "j2", "j3", "j4", "j5"}
	for _, id := range ids {
		seedLearning(t, s, id, "org1")
	}
	cp := &capturePlanner{}
	svc := NewService(s, cp, nil)
	svc.indexSync = true

	if _, err := svc.SubmitPlan(context.Background(), PlanRequest{
		OrgID: "org1", Task: "t", RepoURL: "https://github.com/owner/repo",
		ReferenceMode: "manual", ReferenceJobIDs: ids,
	}); err != nil {
		t.Fatalf("SubmitPlan: %v", err)
	}
	if got := len(cp.last.ResolvedLearnings); got != 3 {
		t.Fatalf("expected resolution capped at 3, got %d", got)
	}
}

// "off" resolves nothing regardless of what's in the store.
func TestSubmitPlanOffResolvesNothing(t *testing.T) {
	s := newTestStore(t)
	seedLearning(t, s, "jobA", "org1")
	cp := &capturePlanner{}
	svc := NewService(s, cp, nil)
	svc.indexSync = true

	if _, err := svc.SubmitPlan(context.Background(), PlanRequest{
		OrgID: "org1", Task: "t", RepoURL: "https://github.com/owner/repo",
		ReferenceMode: "off",
	}); err != nil {
		t.Fatalf("SubmitPlan: %v", err)
	}
	if got := len(cp.last.ResolvedLearnings); got != 0 {
		t.Fatalf("expected no learnings for mode=off, got %d", got)
	}
}

// Auto mode embeds the task exactly once: the query embedding is reused for the
// indexing write rather than being recomputed.
func TestSubmitPlanAutoEmbedsOnce(t *testing.T) {
	s := newTestStore(t)
	emb := &countingEmbedder{}
	svc := NewService(s, &capturePlanner{}, emb)
	svc.indexSync = true

	if _, err := svc.SubmitPlan(context.Background(), PlanRequest{
		OrgID: "org1", Task: "t", RepoURL: "https://github.com/owner/repo",
		ReferenceMode: "auto",
	}); err != nil {
		t.Fatalf("SubmitPlan: %v", err)
	}
	if emb.n != 1 {
		t.Fatalf("expected exactly 1 embed call in auto mode, got %d", emb.n)
	}
}
