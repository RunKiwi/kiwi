// SPDX-License-Identifier: LicenseRef-Kiwi-BSL-1.1
// Copyright (c) 2026 RunKiwi. Licensed under the Business Source License 1.1.
// See ee/LICENSE. This is Control Plane code and is NOT Apache-2.0.

package fleethost

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	compute "google.golang.org/api/compute/v1"
)

// gceController manages a Compute Engine instance via the cloud API. It needs
// compute.instances.{get,start,stop} on that instance — grant it narrowly, on
// the single instance, rather than project-wide.
type gceController struct {
	cfg Config
	svc *compute.InstancesService

	// mu guards startedAt, which suppresses duplicate start calls. Every submit
	// while the host is booting would otherwise fire its own instances.start;
	// they are individually harmless but they rate-limit and they bury the
	// interesting log lines.
	mu        sync.Mutex
	startedAt time.Time
}

// startSuppression is how long after requesting a start we assume the host is
// coming up and skip further start calls. Comfortably longer than a boot.
const startSuppression = 2 * time.Minute

func newGCE(ctx context.Context, cfg Config) (*gceController, error) {
	svc, err := compute.NewService(ctx)
	if err != nil {
		return nil, fmt.Errorf("compute client: %w", err)
	}
	return &gceController{cfg: cfg, svc: compute.NewInstancesService(svc)}, nil
}

func (g *gceController) Enabled() bool { return true }

func (g *gceController) Running(ctx context.Context) (bool, error) {
	inst, err := g.svc.Get(g.cfg.Project, g.cfg.Zone, g.cfg.Instance).Context(ctx).Do()
	if err != nil {
		return false, fmt.Errorf("get instance %s: %w", g.cfg.Instance, err)
	}
	// STAGING and PROVISIONING mean a start is already under way; treating them
	// as running is what keeps Ensure idempotent during a boot.
	switch inst.Status {
	case "RUNNING", "STAGING", "PROVISIONING":
		return true, nil
	default:
		return false, nil
	}
}

func (g *gceController) Ensure(ctx context.Context) error {
	g.mu.Lock()
	if time.Since(g.startedAt) < startSuppression {
		g.mu.Unlock()
		return nil // a start is already in flight
	}
	g.mu.Unlock()

	running, err := g.Running(ctx)
	if err != nil {
		return err
	}
	if running {
		return nil
	}

	g.mu.Lock()
	g.startedAt = time.Now()
	g.mu.Unlock()

	slog.Info("fleethost: starting host", "instance", g.cfg.Instance, "zone", g.cfg.Zone)
	if _, err := g.svc.Start(g.cfg.Project, g.cfg.Zone, g.cfg.Instance).Context(ctx).Do(); err != nil {
		// Clear the suppression so the next submit retries rather than waiting
		// out a window on a start that never happened.
		g.mu.Lock()
		g.startedAt = time.Time{}
		g.mu.Unlock()
		return fmt.Errorf("start instance %s: %w", g.cfg.Instance, err)
	}
	return nil
}

func (g *gceController) Stop(ctx context.Context) error {
	running, err := g.Running(ctx)
	if err != nil {
		return err
	}
	if !running {
		return nil
	}
	slog.Info("fleethost: stopping idle host", "instance", g.cfg.Instance, "zone", g.cfg.Zone)
	if _, err := g.svc.Stop(g.cfg.Project, g.cfg.Zone, g.cfg.Instance).Context(ctx).Do(); err != nil {
		return fmt.Errorf("stop instance %s: %w", g.cfg.Instance, err)
	}
	return nil
}
