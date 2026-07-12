package checkpoint

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/ibreakthecloud/kiwi/pkg/store"
	"gorm.io/gorm"
)

// maxSeqAttempts bounds the optimistic retry when two writers race for the same
// per-job seq.
const maxSeqAttempts = 8

// Service owns the append-only event log and durable checkpoints for a job.
// It is backed by the control-plane Store (Postgres/SQLite) for the ordered
// event/checkpoint metadata and a Snapshotter for the workspace blob (issue #36).
type Service struct {
	store store.Store
	snap  Snapshotter
}

func NewService(s store.Store, snap Snapshotter) *Service {
	return &Service{store: s, snap: snap}
}

// AppendEvent writes the next event for a job with a per-job monotonic seq.
//
// seq is derived from MAX(seq)+1, so two writers racing on the same job can
// compute the same next value. The store's UNIQUE(job_id, seq) index rejects
// the loser with a duplicate-key error; we treat that as a signal to recompute
// and retry rather than an error, giving correct monotonic seqs without a
// per-job lock. Non-uniqueness errors are returned immediately.
func (s *Service) AppendEvent(ctx context.Context, jobID, phase string, payload map[string]interface{}) (*store.Event, error) {
	if payload == nil {
		payload = map[string]interface{}{}
	}
	var lastErr error
	for attempt := 0; attempt < maxSeqAttempts; attempt++ {
		next, err := s.nextSeq(ctx, jobID)
		if err != nil {
			return nil, err
		}
		ev := &store.Event{JobID: jobID, Seq: next, Phase: phase, Payload: payload}
		err = s.store.AppendEvent(ctx, ev)
		if err == nil {
			return ev, nil
		}
		if !isUniqueViolation(err) {
			return nil, err
		}
		lastErr = err // another writer took this seq; recompute and retry
	}
	return nil, fmt.Errorf("append event for job %s: exhausted %d seq retries: %w", jobID, maxSeqAttempts, lastErr)
}

// isUniqueViolation reports whether err is a unique/duplicate-key constraint
// failure, across the drivers we run (Postgres in prod, SQLite in tests).
func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return true
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "unique constraint") || // sqlite: "UNIQUE constraint failed"
		strings.Contains(msg, "duplicate key") || // postgres text
		strings.Contains(msg, "23505") // postgres SQLSTATE unique_violation
}

// Events returns the full ordered event log for a job (for replay).
func (s *Service) Events(ctx context.Context, jobID string) ([]store.Event, error) {
	var evs []store.Event
	err := s.store.DB().WithContext(ctx).
		Where("job_id = ?", jobID).Order("seq asc").Find(&evs).Error
	return evs, err
}

// Write snapshots the workspace and records a checkpoint anchored at the
// current head of the event log.
func (s *Service) Write(ctx context.Context, jobID, dir string, state map[string]interface{}) (*store.Checkpoint, error) {
	uri, hash, err := s.snap.Snapshot(dir)
	if err != nil {
		return nil, err
	}
	head, err := s.headSeq(ctx, jobID)
	if err != nil {
		return nil, err
	}
	if state == nil {
		state = map[string]interface{}{}
	}
	cp := &store.Checkpoint{
		ID:           fmt.Sprintf("%s-cp-%d", jobID, head),
		JobID:        jobID,
		EventSeq:     head,
		SnapshotURI:  &uri,
		SnapshotHash: &hash,
		State:        state,
	}
	if err := s.store.SaveCheckpoint(ctx, cp); err != nil {
		return nil, err
	}
	return cp, nil
}

// Latest returns the most recent checkpoint for a job (highest event_seq).
func (s *Service) Latest(ctx context.Context, jobID string) (*store.Checkpoint, error) {
	var cp store.Checkpoint
	err := s.store.DB().WithContext(ctx).
		Where("job_id = ?", jobID).Order("event_seq desc").First(&cp).Error
	if err != nil {
		return nil, err
	}
	return &cp, nil
}

// Restore materializes the latest checkpoint's snapshot into dir and returns
// the checkpoint (whose EventSeq marks where replay should resume from).
func (s *Service) Restore(ctx context.Context, jobID, dir string) (*store.Checkpoint, error) {
	cp, err := s.Latest(ctx, jobID)
	if err != nil {
		return nil, err
	}
	if cp.SnapshotURI == nil {
		return nil, fmt.Errorf("checkpoint %s has no snapshot", cp.ID)
	}
	if err := s.snap.Restore(*cp.SnapshotURI, dir); err != nil {
		return nil, err
	}
	return cp, nil
}

// headSeq returns the highest event seq for a job (0 if none).
func (s *Service) headSeq(ctx context.Context, jobID string) (int64, error) {
	var seq int64
	err := s.store.DB().WithContext(ctx).Model(&store.Event{}).
		Where("job_id = ?", jobID).Select("COALESCE(MAX(seq),0)").Scan(&seq).Error
	return seq, err
}

func (s *Service) nextSeq(ctx context.Context, jobID string) (int64, error) {
	head, err := s.headSeq(ctx, jobID)
	return head + 1, err
}
