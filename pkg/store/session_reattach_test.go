package store

import (
	"context"
	"errors"
	"testing"
)

// The move that makes a continuation a continuation.
//
// handleDaemonSessionLoad resolves a session by task_id, so without this the
// new task finds no session, starts from round zero, and the Architect's whole
// history is thrown away — silently, with a pull request to show for it.
func TestReattachSessionMovesItToTheNewTask(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	if err := s.SaveAgentSession(ctx, sampleSession("sess_1", "org1", "t1"), nil); err != nil {
		t.Fatal(err)
	}
	if err := s.FinishAgentSession(ctx, "org1", "sess_1", SessionSucceeded); err != nil {
		t.Fatal(err)
	}

	if err := s.ReattachSession(ctx, "org1", "sess_1", "t2"); err != nil {
		t.Fatal(err)
	}

	got, err := s.GetAgentSessionByTask(ctx, "org1", "t2")
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != "sess_1" {
		t.Errorf("session id = %q, want sess_1 — the thread keeps one session", got.ID)
	}
	if got.Round != 1 {
		t.Errorf("round = %d, want the checkpoint's 1 — the position must survive", got.Round)
	}
	if _, err := s.GetAgentSessionByTask(ctx, "org1", "t1"); !errors.Is(err, ErrSessionNotFound) {
		t.Error("the previous task must no longer own the session")
	}
}

// A concluded session is not resumed by an ordinary re-lease — that rule is
// what stops a retry handing the Architect a history ending in a verdict it
// already gave. Reopening is therefore something only a deliberate
// continuation may do, and it happens here.
func TestReattachSessionReopensAConcludedSession(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	if err := s.SaveAgentSession(ctx, sampleSession("sess_1", "org1", "t1"), nil); err != nil {
		t.Fatal(err)
	}
	if err := s.FinishAgentSession(ctx, "org1", "sess_1", SessionSucceeded); err != nil {
		t.Fatal(err)
	}

	if err := s.ReattachSession(ctx, "org1", "sess_1", "t2"); err != nil {
		t.Fatal(err)
	}

	got, err := s.GetAgentSessionByTask(ctx, "org1", "t2")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != SessionRunning {
		t.Errorf("status = %q, want RUNNING", got.Status)
	}
}

// Org scoping, as everywhere else: a session id guessed by another tenant must
// not be movable.
func TestReattachSessionIsOrgScoped(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	if err := s.SaveAgentSession(ctx, sampleSession("sess_1", "org1", "t1"), nil); err != nil {
		t.Fatal(err)
	}
	if err := s.ReattachSession(ctx, "org2", "sess_1", "t2"); err == nil {
		t.Error("expected an error when the org does not own the session")
	}
}

// A silent no-op here is the worst outcome available: the continuation runs
// from scratch, the pull request is wrong, and nothing anywhere says why.
func TestReattachSessionReportsAMissingSession(t *testing.T) {
	s := newTestStore(t)
	if err := s.ReattachSession(context.Background(), "org1", "sess_nope", "t2"); err == nil {
		t.Error("expected an error for a session that does not exist")
	}
}
