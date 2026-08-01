package store

import (
	"context"
	"errors"
	"testing"
)

func sampleSession(id, org, task string) *AgentSession {
	return &AgentSession{
		ID: id, OrgID: org, TaskID: task, JobID: "job_1",
		BaseSHA: "base", HeadSHA: "base", Round: 1,
		State: map[string]interface{}{"round": 1},
	}
}

// A task's first lease has no session, and that is the normal case rather than
// a fault — the daemon uses it to decide between starting and resuming.
func TestMissingSessionIsReportedAsNotFound(t *testing.T) {
	s := newTestStore(t)
	_, err := s.GetAgentSessionByTask(context.Background(), "org1", "task_1")
	if !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("expected ErrSessionNotFound, got %v", err)
	}
}

func TestSaveThenLoadRoundTripsTheCheckpoint(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	sess := sampleSession("sess_1", "org1", "task_1")
	events := []AgentSessionEvent{{Round: 1, Seq: 0, Kind: "round_start", Outcome: "ok"}}
	if err := s.SaveAgentSession(ctx, sess, events); err != nil {
		t.Fatalf("SaveAgentSession: %v", err)
	}

	got, err := s.GetAgentSessionByTask(ctx, "org1", "task_1")
	if err != nil {
		t.Fatalf("GetAgentSessionByTask: %v", err)
	}
	if got.ID != "sess_1" || got.Round != 1 || got.BaseSHA != "base" {
		t.Errorf("round-tripped session = %+v", got)
	}
	if got.State["round"] == nil {
		t.Error("the opaque checkpoint state must survive the round trip")
	}
}

// A checkpoint updates in place. Rounds advance the same row rather than
// accumulating one per round: the row is a position, the events are the history.
func TestSavingAgainAdvancesTheSameSession(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	if err := s.SaveAgentSession(ctx, sampleSession("sess_1", "org1", "task_1"), nil); err != nil {
		t.Fatal(err)
	}

	next := sampleSession("sess_1", "org1", "task_1")
	next.Round = 2
	next.HeadSHA = "round1"
	if err := s.SaveAgentSession(ctx, next, []AgentSessionEvent{{Round: 1, Seq: 1, Kind: "review", Outcome: "revise"}}); err != nil {
		t.Fatal(err)
	}

	got, err := s.GetAgentSessionByTask(ctx, "org1", "task_1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Round != 2 || got.HeadSHA != "round1" {
		t.Errorf("session did not advance: %+v", got)
	}

	events, err := s.ListAgentSessionEvents(ctx, "org1", "sess_1")
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0].Kind != "review" {
		t.Errorf("events = %+v", events)
	}
}

// A daemon that checkpointed successfully but lost the response will retry. The
// retry has to be a no-op rather than an error that fails a healthy run.
func TestReplayedCheckpointDoesNotDuplicateEvents(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	events := []AgentSessionEvent{
		{Round: 1, Seq: 0, Kind: "round_start"},
		{Round: 1, Seq: 1, Kind: "verify", Outcome: "pass"},
	}

	if err := s.SaveAgentSession(ctx, sampleSession("sess_1", "org1", "task_1"), events); err != nil {
		t.Fatal(err)
	}
	replay := []AgentSessionEvent{
		{Round: 1, Seq: 0, Kind: "round_start"},
		{Round: 1, Seq: 1, Kind: "verify", Outcome: "pass"},
	}
	if err := s.SaveAgentSession(ctx, sampleSession("sess_1", "org1", "task_1"), replay); err != nil {
		t.Fatalf("a replayed checkpoint must not fail: %v", err)
	}

	got, err := s.ListAgentSessionEvents(ctx, "org1", "sess_1")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 events after the replay, got %d", len(got))
	}
}

// Events order by (round, seq) so a session's history reads in the order it
// happened rather than in insertion order.
func TestEventsAreOrderedByRoundThenSequence(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	if err := s.SaveAgentSession(ctx, sampleSession("sess_1", "org1", "task_1"), []AgentSessionEvent{
		{Round: 2, Seq: 0, Kind: "second-round-first"},
		{Round: 1, Seq: 1, Kind: "first-round-second"},
		{Round: 1, Seq: 0, Kind: "first-round-first"},
	}); err != nil {
		t.Fatal(err)
	}

	got, err := s.ListAgentSessionEvents(ctx, "org1", "sess_1")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"first-round-first", "first-round-second", "second-round-first"}
	for i, w := range want {
		if got[i].Kind != w {
			t.Errorf("event %d = %q, want %q", i, got[i].Kind, w)
		}
	}
}

// Another tenant's session must be invisible, like every other org-scoped row.
func TestSessionsAreOrgScoped(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	if err := s.SaveAgentSession(ctx, sampleSession("sess_1", "org1", "task_1"), nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetAgentSessionByTask(ctx, "org2", "task_1"); !errors.Is(err, ErrSessionNotFound) {
		t.Fatalf("another org must not see the session, got %v", err)
	}
}

// A concluded session must stop being resumable: a task leased again after its
// session ended is a retry, not a continuation.
func TestFinishRecordsTerminalStatus(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	if err := s.SaveAgentSession(ctx, sampleSession("sess_1", "org1", "task_1"), nil); err != nil {
		t.Fatal(err)
	}
	if err := s.FinishAgentSession(ctx, "org1", "sess_1", SessionSucceeded); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetAgentSessionByTask(ctx, "org1", "task_1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != SessionSucceeded {
		t.Errorf("status = %q", got.Status)
	}

	if err := s.FinishAgentSession(ctx, "org1", "sess_1", "MAYBE"); err == nil {
		t.Error("a non-terminal status must be rejected")
	}
}

func TestSaveRejectsAnIncompleteSession(t *testing.T) {
	s := newTestStore(t)
	if err := s.SaveAgentSession(context.Background(), &AgentSession{ID: "x"}, nil); err == nil {
		t.Fatal("a session without an org and task must be rejected")
	}
}
