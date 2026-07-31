package orchestrator

import (
	"context"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/ibreakthecloud/kiwi/pkg/store"
	"github.com/ibreakthecloud/kiwi/pkg/ver"
	"gorm.io/gorm"
)

func newEventsServer(t *testing.T) *Server {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&TaskEvent{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return &Server{db: db, storage: store.NewPostgresStore(db)}
}

// Telemetry is the only record of what happened inside the Actor–Critic loop:
// the daemon runs it in its own process, so nothing else observes it. Every
// insert failed a foreign key to task_states — the table tasks lived in before
// the daemon seam moved them to queued_tasks — and because the write is
// best-effort by design, the failure was logged and swallowed. The result was a
// total telemetry outage that nothing surfaced: execution records assembled with
// empty worker steps, and a drawer with nothing to show.
//
// This exercises the real write path against a schema without the stale
// constraint, so a reintroduced FK to a table the live path does not use fails
// here rather than in production.
func TestRecordTaskEvents_PersistsAndReadsBack(t *testing.T) {
	s := newEventsServer(t)
	ctx := context.Background()

	events := []ver.TaskEvent{
		{Step: 0, Phase: "initial_test", Outcome: "pass", Detail: "ok", DurationMs: 68000},
		{Step: 1, Phase: "actor", Outcome: "proposed", DurationMs: 4200, InputTokens: 900, OutputTokens: 300},
		{Step: 1, Phase: "critic", Outcome: "rejected", Detail: "truncated mid-string on line 42"},
	}

	s.recordTaskEvents(ctx, "org-1", "job_1-w1", events)

	got := s.taskEventsFor(ctx, "org-1", "job_1-w1")
	if len(got) != len(events) {
		t.Fatalf("persisted %d events, read back %d — the telemetry pipeline is broken", len(events), len(got))
	}
	// Execution order is what makes the timeline readable.
	if got[0].Phase != "initial_test" || got[2].Phase != "critic" {
		t.Errorf("events came back out of order: %+v", got)
	}
	if got[2].Outcome != "rejected" {
		t.Errorf("outcome lost: %+v", got[2])
	}
}

// The Critic's reasons are the only place a run explains itself in words, so a
// rejection must survive the round trip intact enough to read.
func TestRecordTaskEvents_KeepsCriticReasons(t *testing.T) {
	s := newEventsServer(t)
	ctx := context.Background()

	reason := "contains Go code, but has a .rs (Rust) file extension"
	s.recordTaskEvents(ctx, "org-1", "job_2-w1", []ver.TaskEvent{
		{Step: 2, Phase: "critic", Outcome: "rejected", Detail: reason},
	})

	got := s.taskEventsFor(ctx, "org-1", "job_2-w1")
	if len(got) != 1 {
		t.Fatalf("expected 1 event, got %d", len(got))
	}
	if !strings.Contains(got[0].Detail, "file extension") {
		t.Errorf("the Critic's reason did not survive: %q", got[0].Detail)
	}
}

// Events are scoped by org on both write and read; one tenant must never see
// another's execution telemetry.
func TestRecordTaskEvents_AreOrgScoped(t *testing.T) {
	s := newEventsServer(t)
	ctx := context.Background()

	s.recordTaskEvents(ctx, "org-1", "shared-id", []ver.TaskEvent{{Step: 0, Phase: "test", Outcome: "pass"}})

	if got := s.taskEventsFor(ctx, "org-2", "shared-id"); len(got) != 0 {
		t.Errorf("another org read %d event(s) for the same task id", len(got))
	}
}

// An empty report is not an error, and must not write a row.
func TestRecordTaskEvents_EmptyIsANoop(t *testing.T) {
	s := newEventsServer(t)
	ctx := context.Background()

	s.recordTaskEvents(ctx, "org-1", "job_3-w1", nil)
	if got := s.taskEventsFor(ctx, "org-1", "job_3-w1"); len(got) != 0 {
		t.Errorf("expected no rows, got %d", len(got))
	}
}
