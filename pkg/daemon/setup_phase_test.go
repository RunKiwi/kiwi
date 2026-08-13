package daemon

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ibreakthecloud/kiwi/pkg/agent"
	"github.com/ibreakthecloud/kiwi/pkg/ver"
)

// A successful step reports itself as the live activity before it runs and
// records one durable Step-0 event with outcome "ok" once it finishes.
func TestReportSetupPhase_Success(t *testing.T) {
	p := &progressReporter{}

	err := reportSetupPhase(p, "install", "install: npm ci", "npm ci", func() error {
		time.Sleep(time.Millisecond)
		return nil
	})
	if err != nil {
		t.Fatalf("reportSetupPhase: %v", err)
	}

	_, phase, _, phaseSince, _ := p.pending()
	if phase != "install: npm ci" {
		t.Errorf("live phase = %q, want %q", phase, "install: npm ci")
	}
	if phaseSince.IsZero() {
		t.Error("phaseSince should have been set")
	}

	events := p.all()
	if len(events) != 1 {
		t.Fatalf("expected 1 durable event, got %d: %+v", len(events), events)
	}
	if events[0].Phase != "install" || events[0].Outcome != "ok" || events[0].Step != 0 {
		t.Errorf("unexpected event: %+v", events[0])
	}
	if events[0].Detail != "npm ci" {
		t.Errorf("Detail = %q, want the fallback %q", events[0].Detail, "npm ci")
	}
	if events[0].DurationMs < 1 {
		t.Errorf("DurationMs = %d, want > 0", events[0].DurationMs)
	}
}

// A failing step records outcome "error" with the failure's own message as
// Detail, not the generic fallback — the fallback exists for the success
// case, where there is nothing more specific to say than the command itself.
func TestReportSetupPhase_Failure(t *testing.T) {
	p := &progressReporter{}

	err := reportSetupPhase(p, "clone", "clone: https://example/repo", "https://example/repo", func() error {
		return errors.New("could not read Username for https://example/repo")
	})
	if err == nil {
		t.Fatal("expected the underlying error to propagate")
	}

	events := p.all()
	if len(events) != 1 {
		t.Fatalf("expected 1 durable event, got %d", len(events))
	}
	if events[0].Outcome != "error" {
		t.Errorf("Outcome = %q, want error", events[0].Outcome)
	}
	if events[0].Detail != "could not read Username for https://example/repo" {
		t.Errorf("Detail = %q, want the error message", events[0].Detail)
	}
}

// A nil reporter (the same convention every other progressReporter method
// follows) must not panic — some callers run without one.
func TestReportSetupPhase_NilReporterIsSafe(t *testing.T) {
	var p *progressReporter
	err := reportSetupPhase(p, "install", "install: npm ci", "npm ci", func() error { return nil })
	if err != nil {
		t.Fatalf("reportSetupPhase with nil reporter: %v", err)
	}
}

// executeTask's early returns must carry whatever the progressReporter has
// already recorded — otherwise a setup-phase failure event (recorded by
// reportSetupPhase, proved above) never reaches the Control Plane, because
// reportResult only ever sees taskResult.events. This exercises the cheapest
// early return, the ID-sanitization check, so it needs no git/docker/creds:
// a pre-populated event on prog is a stand-in for what a real clone/install
// failure would have added before executeTask got this far.
func TestExecuteTask_EarlyReturnCarriesProgressEvents(t *testing.T) {
	p := &progressReporter{}
	p.add(ver.TaskEvent{Phase: "clone", Outcome: "error", Detail: "could not read Username"})

	d := &Daemon{}
	spec := agent.WorkerSpec{ID: "bad id with spaces"}

	res := d.executeTask(context.Background(), spec, nil, p, "lease-1")

	if res.detail != "invalid task ID format" {
		t.Fatalf("detail = %q, want %q", res.detail, "invalid task ID format")
	}
	if len(res.events) != 1 {
		t.Fatalf("events = %+v, want the 1 pre-populated event to survive", res.events)
	}
	if res.events[0].Phase != "clone" || res.events[0].Outcome != "error" {
		t.Errorf("unexpected event: %+v", res.events[0])
	}
}
