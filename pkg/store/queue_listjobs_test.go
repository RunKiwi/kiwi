package store

import (
	"context"
	"testing"
)

func TestListJobsSurfacesTaskAndRepo(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// A planner-produced task carries the overall goal ("job_task") and repo,
	// plus a fleet and (once leased) the daemon that ran it.
	daemon := "dmn_31eb64d8e5"
	if err := s.EnqueueTask(ctx, &QueuedTask{
		ID:       "job1-w1",
		OrgID:    "org1",
		JobID:    "job1",
		FleetID:  "fleet_a",
		LeasedBy: &daemon,
		Spec: map[string]interface{}{
			"id":       "job1-w1",
			"task":     "worker: patch handler",
			"job_task": "Fix stale data on /api/report + regression test",
			"repo_url": "https://github.com/acme/api.git",
		},
	}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	jobs, err := s.ListJobs(ctx, "org1")
	if err != nil {
		t.Fatalf("ListJobs: %v", err)
	}
	if len(jobs) != 1 {
		t.Fatalf("expected 1 job, got %d", len(jobs))
	}
	if got, want := jobs[0].Task, "Fix stale data on /api/report + regression test"; got != want {
		t.Errorf("Task = %q, want %q", got, want)
	}
	if got, want := jobs[0].Repo, "acme/api"; got != want {
		t.Errorf("Repo = %q, want %q", got, want)
	}
	if got, want := jobs[0].FleetID, "fleet_a"; got != want {
		t.Errorf("FleetID = %q, want %q", got, want)
	}
	if got, want := jobs[0].DaemonID, daemon; got != want {
		t.Errorf("DaemonID = %q, want %q", got, want)
	}
}

func TestListJobsFallsBackToWorkerTask(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// No job_task (e.g. a non-planner enqueue path) — fall back to worker task.
	if err := s.EnqueueTask(ctx, &QueuedTask{
		ID:    "job2-w1",
		OrgID: "org1",
		JobID: "job2",
		Spec:  map[string]interface{}{"id": "job2-w1", "task": "just the worker task"},
	}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	jobs, err := s.ListJobs(ctx, "org1")
	if err != nil {
		t.Fatalf("ListJobs: %v", err)
	}
	if len(jobs) != 1 || jobs[0].Task != "just the worker task" {
		t.Fatalf("expected fallback to worker task, got %+v", jobs)
	}
	if jobs[0].Repo != "" {
		t.Errorf("Repo = %q, want empty when no repo_url", jobs[0].Repo)
	}
}

func TestShortRepo(t *testing.T) {
	cases := map[string]string{
		"https://github.com/acme/api.git": "acme/api",
		"https://github.com/acme/api":     "acme/api",
		"git@github.com:acme/api.git":     "acme/api",
		"http://gitlab.com/g/sub/proj":    "sub/proj",
		"acme/api":                        "acme/api",
		"":                                "",
		"   ":                             "",
		"singlepart":                      "singlepart",
	}
	for in, want := range cases {
		if got := shortRepo(in); got != want {
			t.Errorf("shortRepo(%q) = %q, want %q", in, got, want)
		}
	}
}
