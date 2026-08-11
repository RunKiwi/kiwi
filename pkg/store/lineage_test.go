package store

import (
	"context"
	"testing"
)

// A task submitted directly is the root of its own thread. This is the case
// that must not need a caller to think about it: every task that existed
// before lineage, and every ordinary submission after it, is a root.
func TestEnqueueDefaultsToItsOwnRoot(t *testing.T) {
	s := newTestStore(t)
	task := &QueuedTask{ID: "t1", OrgID: "org1", JobID: "job1", Spec: map[string]interface{}{}}
	if err := s.EnqueueTask(context.Background(), task); err != nil {
		t.Fatal(err)
	}

	got, err := s.ThreadTasks(context.Background(), "org1", "t1")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d tasks, want 1", len(got))
	}
	if got[0].RootTaskID != "t1" {
		t.Errorf("root = %q, want t1", got[0].RootTaskID)
	}
	if got[0].ParentTaskID != nil {
		t.Errorf("parent = %v, want nil", *got[0].ParentTaskID)
	}
	if got[0].Origin != OriginSubmit {
		t.Errorf("origin = %q, want %q", got[0].Origin, OriginSubmit)
	}
}

// SubmitPlan builds its tasks with tx.Create inside the manifest transaction,
// not through EnqueueTask. Defaulting in the store helper alone would have
// left every real task with no thread at all.
func TestARawCreateStillGetsAThread(t *testing.T) {
	s := newTestStore(t)
	if err := s.db.Create(&QueuedTask{ID: "t1", OrgID: "org1", Spec: map[string]interface{}{}}).Error; err != nil {
		t.Fatal(err)
	}
	got, err := s.ThreadTasks(context.Background(), "org1", "t1")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].RootTaskID != "t1" || got[0].Origin != OriginSubmit {
		t.Fatalf("got %+v, want a self-rooted submit task", got)
	}
}

func TestThreadTasksReturnsTheWholeThreadInOrder(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	if err := s.EnqueueTask(ctx, &QueuedTask{ID: "t1", OrgID: "org1", JobID: "job1", Spec: map[string]interface{}{}}); err != nil {
		t.Fatal(err)
	}
	parent := "t1"
	if err := s.EnqueueTask(ctx, &QueuedTask{
		ID: "t2", OrgID: "org1", JobID: "job1", Spec: map[string]interface{}{},
		ParentTaskID: &parent, RootTaskID: "t1", Origin: OriginPRComment,
	}); err != nil {
		t.Fatal(err)
	}

	got, err := s.ThreadTasks(ctx, "org1", "t1")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ID != "t1" || got[1].ID != "t2" {
		t.Fatalf("got %d tasks (%v), want t1 then t2", len(got), taskIDs(got))
	}
	if got[1].Origin != OriginPRComment {
		t.Errorf("origin = %q, want %q", got[1].Origin, OriginPRComment)
	}
}

// A fork is a second child of the same parent, and the model has to express it
// today even though nothing creates one yet — otherwise adding forking later
// is a migration rather than a feature.
func TestAThreadCanBranch(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	if err := s.EnqueueTask(ctx, &QueuedTask{ID: "t1", OrgID: "org1", Spec: map[string]interface{}{}}); err != nil {
		t.Fatal(err)
	}
	parent := "t1"
	for _, id := range []string{"t2", "t3"} {
		if err := s.EnqueueTask(ctx, &QueuedTask{
			ID: id, OrgID: "org1", Spec: map[string]interface{}{},
			ParentTaskID: &parent, RootTaskID: "t1", Origin: OriginFork,
		}); err != nil {
			t.Fatal(err)
		}
	}

	got, err := s.ThreadTasks(ctx, "org1", "t1")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("got %d tasks, want 3 (a parent with two children)", len(got))
	}
}

// Org scoping, as everywhere else: a root id guessed by another tenant must
// return nothing at all.
func TestThreadTasksIsOrgScoped(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	if err := s.EnqueueTask(ctx, &QueuedTask{ID: "t1", OrgID: "org1", Spec: map[string]interface{}{}}); err != nil {
		t.Fatal(err)
	}

	got, err := s.ThreadTasks(ctx, "org2", "t1")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("got %d tasks for the wrong org, want 0", len(got))
	}
}

// The guard that stops a second review comment buying a second concurrent
// round. Two tasks on one job branch race to force-push, and the loser's work
// disappears with no error anywhere.
func TestActiveTaskInThreadFindsUnfinishedWork(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	// One store, distinct ids per case: newTestStore keys the database on the
	// test name, so every call inside one test shares the same rows.
	for _, status := range []string{TaskQueued, TaskLeased} {
		id := "t_" + status
		if err := s.EnqueueTask(ctx, &QueuedTask{
			ID: id, OrgID: "org1", Status: status, Spec: map[string]interface{}{},
		}); err != nil {
			t.Fatal(err)
		}

		active, err := s.ActiveTaskInThread(ctx, "org1", id)
		if err != nil {
			t.Fatal(err)
		}
		if active == nil || active.ID != id {
			t.Fatalf("status %s: got %v, want the unfinished task", status, active)
		}
	}
}

func TestActiveTaskInThreadIgnoresFinishedTasks(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	for _, status := range []string{TaskSucceeded, TaskFailed} {
		id := "t_" + status
		if err := s.EnqueueTask(ctx, &QueuedTask{
			ID: id, OrgID: "org1", Status: status, Spec: map[string]interface{}{},
		}); err != nil {
			t.Fatal(err)
		}

		active, err := s.ActiveTaskInThread(ctx, "org1", id)
		if err != nil {
			t.Fatal(err)
		}
		if active != nil {
			t.Fatalf("status %s: got %+v, want nil", status, active)
		}
	}
}

// GitHub redelivers webhooks. A redelivery must not buy a second round, and
// the database is the only place that can promise it across two replicas.
func TestTriggerCommentIDIsUnique(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	comment := int64(998877)

	if err := s.EnqueueTask(ctx, &QueuedTask{
		ID: "t1", OrgID: "org1", Spec: map[string]interface{}{}, TriggerCommentID: &comment,
	}); err != nil {
		t.Fatal(err)
	}
	err := s.EnqueueTask(ctx, &QueuedTask{
		ID: "t2", OrgID: "org1", Spec: map[string]interface{}{}, TriggerCommentID: &comment,
	})
	if err == nil {
		t.Fatal("a second task for the same comment was accepted")
	}
}

func taskIDs(tasks []QueuedTask) []string {
	out := make([]string, 0, len(tasks))
	for _, t := range tasks {
		out = append(out, t.ID)
	}
	return out
}

// RetryJob re-queues every failed task in a job. Once a job can hold a thread,
// that means the original task and every failed continuation are re-queued at
// once — several tasks sharing one branch, all force-pushing to it, which is
// the exact hazard ActiveTaskInThread exists to prevent. Only the thread's
// newest attempt should come back.
func TestRetryJobRequeuesOnlyTheNewestTaskInAThread(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	root := &QueuedTask{ID: "t1", OrgID: "org1", JobID: "job1", Status: TaskFailed, Spec: map[string]interface{}{}}
	if err := s.EnqueueTask(ctx, root); err != nil {
		t.Fatal(err)
	}
	parent := "t1"
	cont := &QueuedTask{
		ID: "t2", OrgID: "org1", JobID: "job1", Status: TaskFailed, Spec: map[string]interface{}{},
		ParentTaskID: &parent, RootTaskID: "t1", Origin: OriginPRComment,
	}
	if err := s.EnqueueTask(ctx, cont); err != nil {
		t.Fatal(err)
	}

	n, err := s.RetryJob(ctx, "org1", "job1")
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("requeued %d tasks, want 1 — two tasks on one branch race to force-push", n)
	}

	tasks, err := s.ThreadTasks(ctx, "org1", "t1")
	if err != nil {
		t.Fatal(err)
	}
	for _, task := range tasks {
		want := TaskFailed
		if task.ID == "t2" {
			want = TaskQueued
		}
		if task.Status != want {
			t.Errorf("task %s status = %q, want %q", task.ID, task.Status, want)
		}
	}
}

// A job with no continuations behaves exactly as it always did.
func TestRetryJobStillRequeuesEveryWorkerOfAPlainJob(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	for _, id := range []string{"w1", "w2"} {
		if err := s.EnqueueTask(ctx, &QueuedTask{
			ID: id, OrgID: "org1", JobID: "job1", Status: TaskFailed, Spec: map[string]interface{}{},
		}); err != nil {
			t.Fatal(err)
		}
	}

	n, err := s.RetryJob(ctx, "org1", "job1")
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Errorf("requeued %d tasks, want 2 — separate workers are separate threads", n)
	}
}

// The list row needs two things the summary did not carry: how many runs a
// thread has had, and what moved it last. Without the second, a thread
// continued an hour ago still reads as "submitted" — the row would be a
// receipt for something that has since moved on.
func TestListJobsReportsContinuations(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	root := &QueuedTask{
		ID: "t1", OrgID: "org1", JobID: "job1", Status: TaskSucceeded,
		Spec: map[string]interface{}{"task": "add a health endpoint"},
	}
	if err := s.EnqueueTask(ctx, root); err != nil {
		t.Fatal(err)
	}
	parent := "t1"
	cont := &QueuedTask{
		ID: "t2", OrgID: "org1", JobID: "job1", Status: TaskQueued,
		ParentTaskID: &parent, RootTaskID: "t1", Origin: OriginPRComment,
		Spec: map[string]interface{}{"task": "rename the handler"},
	}
	if err := s.EnqueueTask(ctx, cont); err != nil {
		t.Fatal(err)
	}

	jobs, err := s.ListJobs(ctx, "org1")
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 1 {
		t.Fatalf("got %d jobs, want 1 — a thread is one row", len(jobs))
	}
	if jobs[0].ContinuationCount != 1 {
		t.Errorf("continuations = %d, want 1", jobs[0].ContinuationCount)
	}
	if jobs[0].LatestOrigin != OriginPRComment {
		t.Errorf("latest origin = %q, want %q", jobs[0].LatestOrigin, OriginPRComment)
	}
}

// A plain job is unchanged: no chip, and it still reads as submitted.
func TestListJobsLeavesAPlainJobAlone(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	for _, id := range []string{"w1", "w2"} {
		if err := s.EnqueueTask(ctx, &QueuedTask{
			ID: id, OrgID: "org1", JobID: "job1", Status: TaskSucceeded,
			Spec: map[string]interface{}{"task": "multi-worker job"},
		}); err != nil {
			t.Fatal(err)
		}
	}

	jobs, err := s.ListJobs(ctx, "org1")
	if err != nil {
		t.Fatal(err)
	}
	if jobs[0].ContinuationCount != 0 {
		t.Errorf("continuations = %d, want 0 — separate workers are not continuations", jobs[0].ContinuationCount)
	}
	if jobs[0].LatestOrigin != OriginSubmit {
		t.Errorf("latest origin = %q, want %q", jobs[0].LatestOrigin, OriginSubmit)
	}
}
