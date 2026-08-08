// SPDX-License-Identifier: LicenseRef-Kiwi-BSL-1.1
// Copyright (c) 2026 RunKiwi. Licensed under the Business Source License 1.1.
// See ee/LICENSE. This is Control Plane code and is NOT Apache-2.0.

package fleethost

import (
	"context"
	"log/slog"
	"time"
)

// ActivityProbe reports whether any work is outstanding anywhere in the
// installation. store.PostgresStore satisfies it via HasActiveTasks.
type ActivityProbe interface {
	HasActiveTasks(ctx context.Context, orgID string) (bool, error)
}

// Sweeper stops the fleet host once the queue has been continuously empty for
// IdleTTL, and is the counterpart to the Ensure call on submit.
//
// The hysteresis is the whole point. Stopping the moment the queue looks empty
// would kill the host in the gap between two tasks — or worse, in the window
// after a task is leased but before its row reflects it. So the sweeper tracks
// the last moment work was observed and only acts after an uninterrupted quiet
// period, and it treats any probe error as activity: when we cannot tell, the
// safe assumption is that the machine is needed.
type Sweeper struct {
	ctrl  Controller
	probe ActivityProbe
	cfg   Config

	// lastActive is when work was last seen. Initialised to now, so a
	// freshly-started Control Plane never stops a host it has not observed yet.
	lastActive time.Time
}

func NewSweeper(ctrl Controller, probe ActivityProbe, cfg Config) *Sweeper {
	return &Sweeper{ctrl: ctrl, probe: probe, cfg: cfg, lastActive: time.Now()}
}

// Start runs the idle sweep on a ticker until ctx is cancelled. It is a no-op
// when no host is configured, so callers can start it unconditionally.
func (s *Sweeper) Start(ctx context.Context) {
	if !s.ctrl.Enabled() {
		slog.Info("fleethost: no host configured; idle sweeper not started")
		return
	}
	slog.Info("fleethost: idle sweeper started", "config", s.cfg.String())

	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := s.SweepOnce(ctx); err != nil {
					slog.Error("fleethost: idle sweep failed", "err", err)
				}
			}
		}
	}()
}

// SweepOnce performs one idle check. Exported so the behaviour is testable
// without waiting on a ticker.
func (s *Sweeper) SweepOnce(ctx context.Context) error {
	active, err := s.probe.HasActiveTasks(ctx, "")
	if err != nil {
		// Fail safe: an unreadable queue must never be read as "idle". Treat the
		// error as activity so the host stays up, and report it.
		s.lastActive = time.Now()
		return err
	}
	if active {
		s.lastActive = time.Now()
		return nil
	}

	if time.Since(s.lastActive) < s.cfg.IdleTTL {
		return nil // quiet, but not for long enough yet
	}

	// Re-check immediately before acting. The gap between the probe above and
	// the stop below is exactly where a newly-submitted task would land, and
	// stopping the host out from under it would strand the submission.
	active, err = s.probe.HasActiveTasks(ctx, "")
	if err != nil {
		s.lastActive = time.Now()
		return err
	}
	if active {
		s.lastActive = time.Now()
		return nil
	}

	// Reset the clock before acting, not after, so the outcome does not change
	// the backoff: a stop that failed waits another full idle window before it is
	// retried, instead of hammering the cloud API once a minute forever.
	s.lastActive = time.Now()
	return s.ctrl.Stop(ctx)
}
