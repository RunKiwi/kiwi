package checkpoint

import (
	"context"
	"fmt"

	"github.com/ibreakthecloud/kiwi/pkg/store"
)

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
// The store's UNIQUE(job_id, seq) index is the backstop against gaps/dupes.
func (s *Service) AppendEvent(ctx context.Context, jobID, phase string, payload map[string]interface{}) (*store.Event, error) {
	next, err := s.nextSeq(ctx, jobID)
	if err != nil {
		return nil, err
	}
	if payload == nil {
		payload = map[string]interface{}{}
	}
	ev := &store.Event{JobID: jobID, Seq: next, Phase: phase, Payload: payload}
	if err := s.store.AppendEvent(ctx, ev); err != nil {
		return nil, err
	}
	return ev, nil
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
