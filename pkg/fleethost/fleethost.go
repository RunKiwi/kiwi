// Package fleethost controls the lifecycle of the machine the free-tier
// provisioner runs on, so it does not have to be paid for around the clock.
//
// The awkwardness this package resolves: Kiwi's work distribution is a *pull*
// model — daemons poll the Control Plane, the Control Plane never dials out to
// them. That is deliberate (it is what lets a BYOC daemon sit behind a firewall
// with no inbound port), but it means the Control Plane has no way to tell a
// stopped machine to wake up.
//
// The resolution is that host lifecycle is not work distribution. Starting a VM
// does not require reaching the daemon; it requires reaching the *cloud API*,
// which the Control Plane can do perfectly well. So the pull model is preserved
// end to end — nothing ever dials the daemon — while the host underneath it
// scales to zero. Once booted, the daemon starts polling on its own and the
// normal pull flow resumes with nothing about it changed.
//
// The cost is cold start: a stopped VM takes roughly a minute to boot, pull the
// daemon image and register before it can lease anything. That latency is
// visible to the user as the `provisioning` blocked reason (see
// store.DiagnoseQueuedTasks), not as an unexplained wait.
package fleethost

import (
	"context"
	"fmt"
	"os"
	"time"
)

// Controller starts and stops the fleet host.
type Controller interface {
	// Ensure starts the host if it is not already running. It returns once the
	// start has been *requested*, not once the host is serving: booting takes
	// tens of seconds and no caller should block on it.
	Ensure(ctx context.Context) error
	// Stop shuts the host down.
	Stop(ctx context.Context) error
	// Running reports whether the host is currently up.
	Running(ctx context.Context) (bool, error)
	// Enabled reports whether this controller manages anything at all.
	Enabled() bool
}

// Config identifies the machine to manage. An empty field disables the whole
// feature — which is the correct default for BYOC (where the customer owns the
// machine) and for local development (where the daemon runs on the host).
type Config struct {
	Project  string
	Zone     string
	Instance string
	// IdleTTL is how long the queue must be continuously empty before the host
	// is stopped. It is a floor, not a timer: the sweeper tracks when work was
	// last seen and only acts after this much uninterrupted quiet, so a gap
	// between two tasks cannot strand a machine mid-job.
	IdleTTL time.Duration
}

// ConfigFromEnv reads the fleet-host settings. Unset = disabled.
func ConfigFromEnv() Config {
	return Config{
		Project:  os.Getenv("KIWI_FLEET_HOST_PROJECT"),
		Zone:     os.Getenv("KIWI_FLEET_HOST_ZONE"),
		Instance: os.Getenv("KIWI_FLEET_HOST_INSTANCE"),
		IdleTTL:  envDuration("KIWI_FLEET_HOST_IDLE_TTL", 20*time.Minute),
	}
}

func (c Config) valid() bool {
	return c.Project != "" && c.Zone != "" && c.Instance != ""
}

func envDuration(key string, def time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			return d
		}
	}
	return def
}

// Noop is the controller used when no host is configured. Every operation
// succeeds and does nothing, so callers need no enabled-check of their own.
type Noop struct{}

func (Noop) Ensure(context.Context) error          { return nil }
func (Noop) Stop(context.Context) error            { return nil }
func (Noop) Running(context.Context) (bool, error) { return true, nil }
func (Noop) Enabled() bool                         { return false }

// New returns a GCE-backed controller when a host is fully configured, and a
// Noop otherwise. It never fails: an unconfigured or unreachable cloud API must
// not stop the Control Plane from booting, because the Control Plane's core job
// (accepting and planning work) does not depend on it.
func New(ctx context.Context, cfg Config) Controller {
	if !cfg.valid() {
		return Noop{}
	}
	gce, err := newGCE(ctx, cfg)
	if err != nil {
		return Noop{}
	}
	return gce
}

// String describes the managed host for logs.
func (c Config) String() string {
	if !c.valid() {
		return "disabled"
	}
	return fmt.Sprintf("%s/%s/%s (idle %s)", c.Project, c.Zone, c.Instance, c.IdleTTL)
}
