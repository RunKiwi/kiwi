// ee/planner/fork_test.go
package planner

import (
	"context"
	"testing"

	"github.com/ibreakthecloud/kiwi/pkg/store"
)

func TestSubmitForkStartsFromTheParentsBranchWithANewJobID(t *testing.T) {
	s := newTestStore(t)
	seedAdmissibleOrg(t, s, "o1")
	svc := NewService(s, NewHeuristicPlanner(), nil)
	ctx := context.Background()

	// The parent as it would exist after an ordinary SubmitPlan: a real
	// QueuedTask row with a job id and a repo_url in its spec.
	parentID := "job_parent-w0"
	parent := &store.QueuedTask{
		ID: parentID, OrgID: "o1", JobID: "job_parent", Origin: store.OriginSubmit, RootTaskID: parentID,
		Spec: map[string]interface{}{"repo_url": "https://github.com/x/y"},
	}
	if err := s.DB().Create(parent).Error; err != nil {
		t.Fatalf("seed parent task: %v", err)
	}

	result, err := svc.SubmitFork(ctx, ForkInput{
		OrgID: "o1", UserID: "u1", ParentTask: parent, Instruction: "try the alternate approach instead",
	})
	if err != nil {
		t.Fatalf("SubmitFork: %v", err)
	}
	if result.JobID == parent.JobID {
		t.Fatal("expected a fork to get its own job id, not reuse the parent's")
	}

	var tasks []store.QueuedTask
	if err := s.DB().Where("job_id = ?", result.JobID).Find(&tasks).Error; err != nil || len(tasks) == 0 {
		t.Fatalf("expected at least one task enqueued under the new job id, got %v err=%v", tasks, err)
	}
	if tasks[0].Origin != store.OriginFork {
		t.Fatalf("expected Origin=fork, got %q", tasks[0].Origin)
	}
	if tasks[0].ParentTaskID == nil || *tasks[0].ParentTaskID != parent.ID {
		t.Fatal("expected ParentTaskID to point back at the source task")
	}
	if tasks[0].RootTaskID != tasks[0].ID {
		t.Fatal("expected a fork to start its own thread (RootTaskID == its own ID), not extend the parent's")
	}
}
