package provisioner

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"os"
	"time"

	"github.com/ibreakthecloud/kiwi/pkg/auth"
	"github.com/ibreakthecloud/kiwi/pkg/store"
)

// Sweep cadence and TTLs. All overridable via env for ops tuning.
const (
	// defaultStaleTTL: a request left in_progress longer than this is assumed to
	// belong to a crashed provisioner and is requeued.
	defaultStaleTTL = 5 * time.Minute
	// defaultIdleTTL: a free daemon whose org has had no task activity for this
	// long is reclaimed (scaled to zero); a later submit cold-starts it again.
	defaultIdleTTL = 15 * time.Minute
	// defaultSweepInterval: how often the stale + idle sweeps run.
	defaultSweepInterval = time.Minute
)

// ReclaimStaleInProgress requeues provisioning requests stuck in_progress longer
// than ttl back to pending, so a request a crashed provisioner left claimed is
// retried. Both provision and reclaim side effects are idempotent (the launcher
// docker-rm's first), so re-running them is safe. Returns the number requeued.
func (p *Provisioner) ReclaimStaleInProgress(ctx context.Context, ttl time.Duration) (int, error) {
	cutoff := time.Now().Add(-ttl)
	res := p.db.WithContext(ctx).Model(&auth.ProvisioningRequest{}).
		Where("status = ? AND updated_at < ?", statusInProgress, cutoff).
		Update("status", statusPending)
	return int(res.RowsAffected), res.Error
}

// ReclaimIdle enqueues a reclaim request for each free-fleet daemon whose org has
// been idle (no queued/leased tasks, and no task activity within idleTTL) so the
// per-org daemon scales to zero. A later submit cold-starts it again. An org with
// an already-open reclaim is skipped. Returns the number enqueued.
func (p *Provisioner) ReclaimIdle(ctx context.Context, idleTTL time.Duration) (int, error) {
	cutoff := time.Now().Add(-idleTTL)

	var orgIDs []string
	if err := p.db.WithContext(ctx).Model(&store.Daemon{}).
		Where("fleet_id = ?", auth.SharedFreeFleet).
		Distinct().Pluck("org_id", &orgIDs).Error; err != nil {
		return 0, err
	}

	enqueued := 0
	for _, orgID := range orgIDs {
		idle, err := p.orgIdle(ctx, orgID, cutoff)
		if err != nil {
			return enqueued, err
		}
		if !idle {
			continue
		}
		ok, err := p.enqueueReclaim(ctx, orgID)
		if err != nil {
			return enqueued, err
		}
		if ok {
			enqueued++
		}
	}
	return enqueued, nil
}

// orgIdle reports whether an org has no active tasks and no task activity since
// cutoff. An org whose free daemon exists but that has never run a task is idle
// once the daemon itself predates the cutoff (provisioned but unused).
func (p *Provisioner) orgIdle(ctx context.Context, orgID string, cutoff time.Time) (bool, error) {
	var active int64
	if err := p.db.WithContext(ctx).Model(&store.QueuedTask{}).
		Where("org_id = ? AND status IN ?", orgID, []string{store.TaskQueued, store.TaskLeased}).
		Count(&active).Error; err != nil {
		return false, err
	}
	if active > 0 {
		return false, nil
	}

	// Most recent task activity for the org, if any. Read the row as a struct
	// (Order/Limit/Find) rather than SELECT MAX(updated_at) into a scalar: SQLite
	// returns datetimes as strings, which a scalar time scan can't parse, whereas
	// GORM parses model timestamp fields on both SQLite and Postgres.
	var lastTask store.QueuedTask
	if err := p.db.WithContext(ctx).
		Where("org_id = ?", orgID).
		Order("updated_at DESC").Limit(1).
		Find(&lastTask).Error; err != nil {
		return false, err
	}
	if lastTask.ID != "" {
		return lastTask.UpdatedAt.Before(cutoff), nil
	}

	// No tasks ever: fall back to the newest free daemon's age.
	var newest store.Daemon
	if err := p.db.WithContext(ctx).
		Where("org_id = ? AND fleet_id = ?", orgID, auth.SharedFreeFleet).
		Order("created_at DESC").Limit(1).
		Find(&newest).Error; err != nil {
		return false, err
	}
	return newest.ID != "" && newest.CreatedAt.Before(cutoff), nil
}

// enqueueReclaim inserts a pending reclaim for the org unless one is already open
// (pending or in_progress). Returns whether a row was enqueued.
func (p *Provisioner) enqueueReclaim(ctx context.Context, orgID string) (bool, error) {
	var open int64
	if err := p.db.WithContext(ctx).Model(&auth.ProvisioningRequest{}).
		Where("org_id = ? AND type = ? AND status IN ?", orgID, "reclaim", []string{statusPending, statusInProgress}).
		Count(&open).Error; err != nil {
		return false, err
	}
	if open > 0 {
		return false, nil
	}

	idBytes := make([]byte, 8)
	rand.Read(idBytes)
	req := auth.ProvisioningRequest{
		ID:        "prov_" + hex.EncodeToString(idBytes),
		OrgID:     orgID,
		Type:      "reclaim",
		Status:    statusPending,
		CreatedAt: time.Now(),
	}
	if err := p.db.WithContext(ctx).Create(&req).Error; err != nil {
		return false, err
	}
	return true, nil
}

// envDuration reads a duration from env, falling back to def on unset/invalid.
func envDuration(key string, def time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
		slog.Warn("provisioner: invalid duration env, using default", "key", key, "value", v, "default", def)
	}
	return def
}
