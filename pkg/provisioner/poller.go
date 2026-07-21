package provisioner

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/ibreakthecloud/kiwi/pkg/auth"
	"github.com/ibreakthecloud/kiwi/pkg/store"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Request lifecycle statuses. A request is claimed (pending -> inProgress) inside
// a short transaction, then its side effects run OUTSIDE any transaction, then it
// is settled (inProgress -> completed|failed) in a second short transaction. This
// keeps slow, side-effecting work (minting tokens, launching containers) off the
// row lock and — critically — makes the terminal status durable: settling in its
// own transaction means a "failed" write is never rolled back by the same error
// that caused it (the bug in the first cut, which left rows stuck pending and
// retried forever, leaking a join token per tick).
const (
	statusPending    = "pending"
	statusInProgress = "in_progress"
	statusCompleted  = "completed"
	statusFailed     = "failed"
)

type Provisioner struct {
	db       *gorm.DB
	store    store.Store
	launcher Launcher
	apiURL   string
}

// NewProvisioner creates a new Provisioner instance.
func NewProvisioner(db *gorm.DB, s store.Store, launcher Launcher, apiURL string) *Provisioner {
	return &Provisioner{
		db:       db,
		store:    s,
		launcher: launcher,
		apiURL:   apiURL,
	}
}

// EnsureSchema creates the partial unique index that makes cold-start enqueue
// idempotent: at most one PENDING provision request may exist per org, so racing
// `kiwi submit`s cannot enqueue duplicates. Idempotent; safe to call on every
// boot. Both Postgres and SQLite support partial indexes with this syntax.
func EnsureSchema(db *gorm.DB) error {
	return db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_prov_one_pending_provision ` +
		`ON provisioning_requests (org_id) WHERE status = 'pending' AND type = 'provision'`).Error
}

// Start ensures the schema, then runs the poller and the sweeper loops in
// background goroutines.
func (p *Provisioner) Start(ctx context.Context) {
	if err := EnsureSchema(p.db); err != nil {
		slog.Error("provisioner: ensure schema failed", "err", err)
	}
	go p.pollLoop(ctx)
	go p.sweepLoop(ctx)
}

// pollLoop drains pending provisioning requests on a short ticker.
func (p *Provisioner) pollLoop(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			for {
				processed, err := p.PollOnce(ctx)
				if err != nil {
					slog.Error("provisioner poll error", "err", err)
					break
				}
				if !processed {
					break // no more pending requests
				}
			}
		}
	}
}

// sweepLoop periodically requeues stale in_progress requests (crashed mid-claim)
// and reclaims idle free daemons (scale-to-zero). Intervals/TTLs are env-tunable.
func (p *Provisioner) sweepLoop(ctx context.Context) {
	interval := envDuration("KIWI_PROVISIONER_SWEEP_INTERVAL", defaultSweepInterval)
	staleTTL := envDuration("KIWI_PROVISIONER_STALE_TTL", defaultStaleTTL)
	idleTTL := envDuration("KIWI_FREE_IDLE_TTL", defaultIdleTTL)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if n, err := p.ReclaimStaleInProgress(ctx, staleTTL); err != nil {
				slog.Error("provisioner: stale sweep failed", "err", err)
			} else if n > 0 {
				slog.Info("provisioner: requeued stale in_progress request(s)", "count", n)
			}
			if n, err := p.ReclaimIdle(ctx, idleTTL); err != nil {
				slog.Error("provisioner: idle reclaim failed", "err", err)
			} else if n > 0 {
				slog.Info("provisioner: enqueued idle reclaim(s)", "count", n)
			}
		}
	}
}

// PollOnce claims one pending ProvisioningRequest, executes its side effects, and
// settles it. Returns true if a request was claimed (regardless of whether its
// side effects succeeded), false if none were available.
func (p *Provisioner) PollOnce(ctx context.Context) (bool, error) {
	req, claimed, err := p.claim(ctx)
	if err != nil || !claimed {
		return false, err
	}

	// Side effects run OUTSIDE any transaction: no row lock is held across a
	// docker launch, and the settle below cannot be rolled back by a failure here.
	status, sideErr := p.execute(ctx, req)
	if sideErr != nil {
		slog.Error("provisioner: request failed", "id", req.ID, "org", req.OrgID, "type", req.Type, "err", sideErr)
	}

	if err := p.settle(ctx, req.ID, status); err != nil {
		// The work is done (or failed) but we couldn't record it. Report the
		// settle error so the caller logs it; the row stays in_progress and is
		// recoverable by a sweep (see reclaimStale, follow-up work).
		return true, fmt.Errorf("settle request %s: %w", req.ID, err)
	}
	return true, nil
}

// claim atomically moves the oldest pending request to in_progress and returns it.
// On Postgres the candidate is locked FOR UPDATE SKIP LOCKED so concurrent
// pollers never claim the same row; on any dialect the conditional update
// (WHERE status = pending) with a RowsAffected check provides the same guarantee.
func (p *Provisioner) claim(ctx context.Context) (auth.ProvisioningRequest, bool, error) {
	var req auth.ProvisioningRequest
	var claimed bool
	err := p.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		q := tx.Where("status = ?", statusPending).Order("created_at ASC")
		if tx.Dialector.Name() == "postgres" {
			q = q.Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"})
		}
		if err := q.First(&req).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil // nothing to do
			}
			return err
		}

		res := tx.Model(&auth.ProvisioningRequest{}).
			Where("id = ? AND status = ?", req.ID, statusPending).
			Updates(map[string]interface{}{"status": statusInProgress})
		if res.Error != nil {
			return res.Error
		}
		// RowsAffected == 0 means another poller claimed it between our read and
		// update; treat as nothing claimed.
		claimed = res.RowsAffected > 0
		return nil
	})
	return req, claimed, err
}

// execute performs a claimed request's side effects and returns the terminal
// status to settle it to. The returned error is for logging only; the status is
// authoritative. A failed request is terminal — it is not retried. Recovery is by
// a fresh provision request (cold-start enqueues one on the next submit, since
// the dedup index only blocks a PENDING duplicate, not a failed one).
func (p *Provisioner) execute(ctx context.Context, req auth.ProvisioningRequest) (string, error) {
	switch req.Type {
	case "provision":
		joinToken, err := p.store.CreateDaemonJoinToken(ctx, req.OrgID, auth.SharedFreeFleet, time.Hour)
		if err != nil {
			return statusFailed, fmt.Errorf("mint join token for org %s: %w", req.OrgID, err)
		}
		if _, err := p.launcher.Launch(ctx, req.OrgID, auth.SharedFreeFleet, joinToken, p.apiURL); err != nil {
			return statusFailed, fmt.Errorf("launch daemon for org %s: %w", req.OrgID, err)
		}
		return statusCompleted, nil
	case "reclaim":
		if err := p.launcher.Stop(ctx, req.OrgID); err != nil {
			return statusFailed, fmt.Errorf("stop daemon for org %s: %w", req.OrgID, err)
		}
		// Deregister the stopped free daemon so it doesn't orphan a row; a later
		// cold-start registers fresh. Non-fatal: the container is already stopped,
		// and Stop is not idempotent on a gone container, so a lingering row is not
		// worth failing (and re-running) the reclaim over.
		if _, err := p.store.DeleteDaemonsByOrgAndFleet(ctx, req.OrgID, auth.SharedFreeFleet); err != nil {
			slog.Warn("provisioner: deregister after reclaim failed", "org", req.OrgID, "err", err)
		}
		return statusCompleted, nil
	default:
		return statusFailed, fmt.Errorf("unknown provisioning request type %q", req.Type)
	}
}

// settle records a request's terminal status in its own transaction, so it is
// durable independent of whatever happened during execute.
func (p *Provisioner) settle(ctx context.Context, id, status string) error {
	return p.db.WithContext(ctx).Model(&auth.ProvisioningRequest{}).
		Where("id = ?", id).
		Updates(map[string]interface{}{"status": status}).Error
}
