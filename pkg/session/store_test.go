package session

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

// memStore is an in-memory Store. It records every save so a test can assert on
// what a crashed daemon would have left behind.
type memStore struct {
	cp       *Checkpoint
	events   []Event
	saves    int
	finished *bool
	loadErr  error
	saveErr  error
}

func (m *memStore) Load(context.Context, string) (*Checkpoint, error) {
	if m.loadErr != nil {
		return nil, m.loadErr
	}
	return m.cp, nil
}

func (m *memStore) Save(_ context.Context, _ string, cp Checkpoint, events []Event) error {
	if m.saveErr != nil {
		return m.saveErr
	}
	m.saves++
	c := cp
	m.cp = &c
	m.events = append(m.events, events...)
	return nil
}

func (m *memStore) Finish(_ context.Context, _ string, success bool) error {
	m.finished = &success
	return nil
}

func durableRunner(t *testing.T, arch Architect, ws *fakeWorkspace, store *memStore) *Runner {
	t.Helper()
	tools, _ := newTools(t)
	return &Runner{
		Architect:   arch,
		Implementer: finishingRunner("done"),
		Tools:       tools,
		Workspace:   ws,
		Verify:      passing("ok"),
		Store:       store,
		SessionID:   "sess_1",
	}
}

func TestCheckpointRecordsTheNextRoundAndItsEvents(t *testing.T) {
	arch := &fakeArchitect{
		plan:    Spec{Verdict: VerdictProceed, Objective: "round one"},
		reviews: []Spec{{Verdict: VerdictApprove, Summary: "ok"}},
	}
	ws := &fakeWorkspace{tree: []string{"a.go"}, diff: "+x", head: "base"}
	store := &memStore{}

	res, err := durableRunner(t, arch, ws, store).Run(context.Background(), Task{Description: "task"})
	if err != nil || !res.Success {
		t.Fatalf("expected success, got %+v err=%v", res, err)
	}
	if store.saves == 0 {
		t.Fatal("a durable session must checkpoint")
	}
	if len(store.events) == 0 {
		t.Fatal("checkpoints must carry the events they belong to")
	}
	if store.finished == nil || !*store.finished {
		t.Fatal("a finished session must record its terminal status")
	}
}

// The point of the checkpoint: a different process picks the task up and
// continues from the last finished round instead of starting over.
func TestResumeSkipsPlanningAndContinuesFromTheCheckpoint(t *testing.T) {
	arch := &fakeArchitect{
		plan:    Spec{Verdict: VerdictProceed, Objective: "should not be called"},
		reviews: []Spec{{Verdict: VerdictApprove, Summary: "ok"}},
	}
	ws := &fakeWorkspace{tree: []string{"a.go"}, diff: "+x", head: "round1"}
	store := &memStore{cp: &Checkpoint{
		Round:   2,
		Spec:    Spec{Verdict: VerdictRevise, Objective: "finish the retry wrapper"},
		BaseSHA: "base",
		HeadSHA: "round1",
		History: []string{"- round 1: asked for \"start the wrapper\""},
	}}

	r := durableRunner(t, arch, ws, store)
	res, err := r.Run(context.Background(), Task{Description: "task"})
	if err != nil || !res.Success {
		t.Fatalf("expected success, got %+v err=%v", res, err)
	}

	if arch.plannedCalls != 0 {
		t.Error("a resumed session must not re-plan: the spec is already in the checkpoint")
	}
	if len(arch.seen) != 1 {
		t.Fatalf("expected one review, got %d", len(arch.seen))
	}
	if arch.seen[0].Round != 2 {
		t.Errorf("resumed at round %d, want 2", arch.seen[0].Round)
	}
	if arch.seen[0].Spec.Objective != "finish the retry wrapper" {
		t.Errorf("the checkpointed spec must drive the resumed round, got %q", arch.seen[0].Spec.Objective)
	}
	if len(arch.seen[0].History) != 1 {
		t.Errorf("history should survive the resume, got %v", arch.seen[0].History)
	}
}

// Resuming discards whatever the interrupted round had half-done, rather than
// trying to continue a partial working tree.
func TestResumeResetsTheWorktreeToTheLastCommittedRound(t *testing.T) {
	arch := &fakeArchitect{reviews: []Spec{{Verdict: VerdictApprove, Summary: "ok"}}}
	ws := &fakeWorkspace{tree: []string{"a.go"}, diff: "+x", head: "half-finished"}
	store := &memStore{cp: &Checkpoint{
		Round: 2, Spec: Spec{Verdict: VerdictRevise, Objective: "continue"},
		BaseSHA: "base", HeadSHA: "round1",
	}}

	if _, err := durableRunner(t, arch, ws, store).Run(context.Background(), Task{Description: "task"}); err != nil {
		t.Fatal(err)
	}
	if ws.head != "sha1" && ws.head != "round1" {
		t.Errorf("the worktree should have been reset to the checkpointed head first, got %q", ws.head)
	}
	if !ws.wasReset {
		t.Error("Reset must be called so the interrupted round is discarded")
	}
}

// A round that takes its daemon down twice is a poison pill. Bounding it here
// rather than leaving it to the queue's MaxLeaseAttempts saves five leases and
// five cold starts spent learning the same thing.
func TestARoundThatKeepsCrashingIsAbandoned(t *testing.T) {
	arch := &fakeArchitect{reviews: []Spec{{Verdict: VerdictApprove}}}
	ws := &fakeWorkspace{tree: []string{"a.go"}, head: "round1"}
	store := &memStore{cp: &Checkpoint{
		Round: 2, Attempts: maxRoundAttempts,
		Spec: Spec{Verdict: VerdictRevise, Objective: "the round that kills daemons"},
	}}

	res, err := durableRunner(t, arch, ws, store).Run(context.Background(), Task{Description: "task"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Success {
		t.Fatal("expected the poison round to fail the session")
	}
	if !strings.Contains(res.Detail, "unrunnable") {
		t.Errorf("detail should explain why it stopped retrying, got %q", res.Detail)
	}
	if len(arch.seen) != 0 {
		t.Error("no round should have run")
	}
	if store.finished == nil || *store.finished {
		t.Error("the session must be recorded as finished-failed so it is not resumed again")
	}
}

// The attempt count has to be written before the round runs. A checkpoint
// written only on success cannot count attempts at all — a round that kills the
// process never gets to write one, so every retry would look like the first.
func TestAttemptIsRecordedBeforeTheRoundRuns(t *testing.T) {
	arch := &fakeArchitect{
		plan:    Spec{Verdict: VerdictProceed, Objective: "round one"},
		reviews: []Spec{{Verdict: VerdictApprove}},
	}
	ws := &fakeWorkspace{tree: []string{"a.go"}, diff: "+x", head: "base"}
	store := &memStore{}

	var duringRound *Checkpoint
	r := durableRunner(t, arch, ws, store)
	r.Verify = func(context.Context) (string, bool, error) {
		// By the time verification runs, round 1 is under way; whatever is
		// checkpointed now is what a crash here would leave behind.
		if store.cp != nil {
			c := *store.cp
			duringRound = &c
		}
		return "ok", true, nil
	}

	if _, err := r.Run(context.Background(), Task{Description: "task"}); err != nil {
		t.Fatal(err)
	}
	if duringRound == nil {
		t.Fatal("no checkpoint existed while the round was running")
	}
	if duringRound.Round != 1 || duringRound.Attempts != 1 {
		t.Errorf("mid-round checkpoint = round %d attempt %d, want round 1 attempt 1", duringRound.Round, duringRound.Attempts)
	}
}

// Durability is insurance. Losing the Control Plane briefly must not fail a
// task that is otherwise going fine.
func TestCheckpointFailuresDoNotFailTheSession(t *testing.T) {
	arch := &fakeArchitect{
		plan:    Spec{Verdict: VerdictProceed, Objective: "round one"},
		reviews: []Spec{{Verdict: VerdictApprove, Summary: "ok"}},
	}
	ws := &fakeWorkspace{tree: []string{"a.go"}, diff: "+x", head: "base"}
	store := &memStore{saveErr: errors.New("control plane unreachable")}

	res, err := durableRunner(t, arch, ws, store).Run(context.Background(), Task{Description: "task"})
	if err != nil {
		t.Fatalf("a failed checkpoint must not fail the run: %v", err)
	}
	if !res.Success {
		t.Fatalf("expected success despite checkpoint failures, got %q", res.Detail)
	}
}

// Likewise an unreadable checkpoint: starting over repeats work, refusing to
// run loses it.
func TestAnUnreadableCheckpointStartsOverRatherThanFailing(t *testing.T) {
	arch := &fakeArchitect{
		plan:    Spec{Verdict: VerdictProceed, Objective: "round one"},
		reviews: []Spec{{Verdict: VerdictApprove, Summary: "ok"}},
	}
	ws := &fakeWorkspace{tree: []string{"a.go"}, diff: "+x", head: "base"}
	store := &memStore{loadErr: fmt.Errorf("boom")}

	res, err := durableRunner(t, arch, ws, store).Run(context.Background(), Task{Description: "task"})
	if err != nil {
		t.Fatalf("expected the session to start from the beginning, got %v", err)
	}
	if !res.Success {
		t.Fatalf("expected success, got %q", res.Detail)
	}
	if arch.plannedCalls != 1 {
		t.Errorf("a session that could not load a checkpoint must plan afresh, planned %d times", arch.plannedCalls)
	}
}

// Without a Store the session runs entirely in memory, which is what a test or
// a single-shot run wants.
func TestSessionRunsWithoutAStore(t *testing.T) {
	arch := &fakeArchitect{
		plan:    Spec{Verdict: VerdictProceed, Objective: "round one"},
		reviews: []Spec{{Verdict: VerdictApprove, Summary: "ok"}},
	}
	ws := &fakeWorkspace{tree: []string{"a.go"}, diff: "+x", head: "base"}
	r, _ := newRunner(t, arch, finishingRunner("done"), ws, passing("ok"))

	res, err := r.Run(context.Background(), Task{Description: "task"})
	if err != nil || !res.Success {
		t.Fatalf("expected success, got %+v err=%v", res, err)
	}
}
