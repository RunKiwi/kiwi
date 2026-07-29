package fleethost

import (
	"context"
	"errors"
	"testing"
	"time"
)

// fakeController records stop calls instead of touching a cloud API.
type fakeController struct {
	stops   int
	starts  int
	stopErr error
}

func (f *fakeController) Ensure(context.Context) error {
	f.starts++
	return nil
}
func (f *fakeController) Stop(context.Context) error {
	f.stops++
	return f.stopErr
}
func (f *fakeController) Running(context.Context) (bool, error) { return true, nil }
func (f *fakeController) Enabled() bool                         { return true }

// fakeProbe reports a scripted activity state.
type fakeProbe struct {
	active bool
	err    error
	calls  int
}

func (p *fakeProbe) HasActiveTasks(context.Context, string) (bool, error) {
	p.calls++
	return p.active, p.err
}

func newSweeper(t *testing.T, ctrl Controller, probe ActivityProbe, idle time.Duration) *Sweeper {
	t.Helper()
	return NewSweeper(ctrl, probe, Config{
		Project: "p", Zone: "z", Instance: "i", IdleTTL: idle,
	})
}

// The core safety property: work in the queue means the host stays up, however
// long the sweeper has been running.
func TestSweepDoesNotStopWhileWorkIsQueued(t *testing.T) {
	ctrl := &fakeController{}
	sw := newSweeper(t, ctrl, &fakeProbe{active: true}, time.Millisecond)
	sw.lastActive = time.Now().Add(-time.Hour) // long past the TTL

	if err := sw.SweepOnce(context.Background()); err != nil {
		t.Fatalf("SweepOnce: %v", err)
	}
	if ctrl.stops != 0 {
		t.Errorf("must not stop a host with active work, got %d stops", ctrl.stops)
	}
}

// Quiet, but not quiet for long enough, is not grounds to stop — otherwise the
// gap between two tasks would kill the machine.
func TestSweepWaitsForTheFullIdleWindow(t *testing.T) {
	ctrl := &fakeController{}
	sw := newSweeper(t, ctrl, &fakeProbe{active: false}, time.Hour)

	if err := sw.SweepOnce(context.Background()); err != nil {
		t.Fatalf("SweepOnce: %v", err)
	}
	if ctrl.stops != 0 {
		t.Errorf("idle window not elapsed; got %d stops", ctrl.stops)
	}
}

func TestSweepStopsAfterSustainedIdle(t *testing.T) {
	ctrl := &fakeController{}
	probe := &fakeProbe{active: false}
	sw := newSweeper(t, ctrl, probe, time.Minute)
	sw.lastActive = time.Now().Add(-2 * time.Minute)

	if err := sw.SweepOnce(context.Background()); err != nil {
		t.Fatalf("SweepOnce: %v", err)
	}
	if ctrl.stops != 1 {
		t.Errorf("stops: got %d, want 1", ctrl.stops)
	}
	// The re-check before acting is what closes the race with a submit landing
	// between the decision and the stop.
	if probe.calls < 2 {
		t.Errorf("expected a confirming re-check before stopping, got %d probes", probe.calls)
	}
}

// Fail safe: if we cannot read the queue we must assume the host is needed.
// Reading an error as "idle" would stop a machine with work on it.
func TestSweepTreatsProbeErrorAsActivity(t *testing.T) {
	ctrl := &fakeController{}
	sw := newSweeper(t, ctrl, &fakeProbe{err: errors.New("db down")}, time.Minute)
	sw.lastActive = time.Now().Add(-time.Hour)

	if err := sw.SweepOnce(context.Background()); err == nil {
		t.Error("a probe failure should be reported, not swallowed")
	}
	if ctrl.stops != 0 {
		t.Errorf("must not stop on an unreadable queue, got %d stops", ctrl.stops)
	}
	// The idle clock must have been reset, so the next sweep starts over rather
	// than stopping the instant the database recovers.
	if time.Since(sw.lastActive) > time.Second {
		t.Error("probe failure should reset the idle clock")
	}
}

// A freshly-booted Control Plane has not observed the queue yet and must not
// stop a host on its very first sweep.
func TestSweeperStartsWithFreshIdleClock(t *testing.T) {
	ctrl := &fakeController{}
	sw := newSweeper(t, ctrl, &fakeProbe{active: false}, time.Minute)

	if err := sw.SweepOnce(context.Background()); err != nil {
		t.Fatalf("SweepOnce: %v", err)
	}
	if ctrl.stops != 0 {
		t.Errorf("a just-started sweeper must not stop anything, got %d stops", ctrl.stops)
	}
}

// A failed stop must not be retried on every tick.
func TestSweepBacksOffAfterFailedStop(t *testing.T) {
	ctrl := &fakeController{stopErr: errors.New("api error")}
	sw := newSweeper(t, ctrl, &fakeProbe{active: false}, time.Minute)
	sw.lastActive = time.Now().Add(-2 * time.Minute)

	if err := sw.SweepOnce(context.Background()); err == nil {
		t.Error("a failed stop should be reported")
	}
	if err := sw.SweepOnce(context.Background()); err != nil {
		t.Fatalf("second sweep: %v", err)
	}
	if ctrl.stops != 1 {
		t.Errorf("stop should not be retried immediately, got %d attempts", ctrl.stops)
	}
}

// With no host configured the sweeper must not run at all, so BYOC and local
// dev are unaffected by this feature existing.
func TestSweeperDoesNothingWhenDisabled(t *testing.T) {
	ctrl := Noop{}
	probe := &fakeProbe{}
	sw := NewSweeper(ctrl, probe, Config{IdleTTL: time.Minute})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sw.Start(ctx)

	if probe.calls != 0 {
		t.Errorf("disabled sweeper should not probe, got %d calls", probe.calls)
	}
}

func TestConfigFromEnvDisabledWhenUnset(t *testing.T) {
	t.Setenv("KIWI_FLEET_HOST_PROJECT", "")
	t.Setenv("KIWI_FLEET_HOST_ZONE", "")
	t.Setenv("KIWI_FLEET_HOST_INSTANCE", "")

	cfg := ConfigFromEnv()
	if cfg.valid() {
		t.Error("unset env must not produce a valid host config")
	}
	if got := New(context.Background(), cfg); got.Enabled() {
		t.Error("unconfigured New should yield a disabled controller")
	}
}

func TestConfigFromEnvReadsIdleTTL(t *testing.T) {
	t.Setenv("KIWI_FLEET_HOST_PROJECT", "p")
	t.Setenv("KIWI_FLEET_HOST_ZONE", "z")
	t.Setenv("KIWI_FLEET_HOST_INSTANCE", "i")
	t.Setenv("KIWI_FLEET_HOST_IDLE_TTL", "45m")

	cfg := ConfigFromEnv()
	if !cfg.valid() {
		t.Fatal("fully-specified env should be valid")
	}
	if cfg.IdleTTL != 45*time.Minute {
		t.Errorf("IdleTTL: got %s, want 45m", cfg.IdleTTL)
	}
}

// A malformed or non-positive TTL must fall back to the default rather than
// becoming zero, which would stop the host the moment the queue emptied.
func TestConfigFromEnvRejectsBadIdleTTL(t *testing.T) {
	for _, v := range []string{"nonsense", "0", "-5m"} {
		t.Setenv("KIWI_FLEET_HOST_IDLE_TTL", v)
		if got := ConfigFromEnv().IdleTTL; got != 20*time.Minute {
			t.Errorf("TTL %q: got %s, want the 20m default", v, got)
		}
	}
}
