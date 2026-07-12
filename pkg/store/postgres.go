package store

import (
	"context"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// PostgresStore implements the Store interface using a PostgreSQL GORM connection.
type PostgresStore struct {
	db *gorm.DB
}

func NewPostgresStore(db *gorm.DB) *PostgresStore {
	return &PostgresStore{db: db}
}

func (s *PostgresStore) GetOrganization(ctx context.Context, id string) (*Organization, error) {
	var org Organization
	if err := s.db.WithContext(ctx).First(&org, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &org, nil
}

func (s *PostgresStore) GetOrgLimits(ctx context.Context, orgID string) (*OrgLimits, error) {
	var limits OrgLimits
	if err := s.db.WithContext(ctx).Where("org_id = ?", orgID).First(&limits).Error; err != nil {
		return nil, err
	}
	return &limits, nil
}

func (s *PostgresStore) ListOrganizations(ctx context.Context) ([]Organization, error) {
	var orgs []Organization
	if err := s.db.WithContext(ctx).Order("created_at desc").Find(&orgs).Error; err != nil {
		return nil, err
	}
	return orgs, nil
}

func (s *PostgresStore) DB() *gorm.DB {
	return s.db
}

func (s *PostgresStore) CreateManifest(ctx context.Context, m *Manifest) error {
	// Use clauses.OnConflict to ignore if it already exists (immutable)
	// Since ID is sha256, it's deterministic.
	return s.db.WithContext(ctx).Clauses(clause.OnConflict{
		DoNothing: true,
	}).Create(m).Error
}

func (s *PostgresStore) UpdateJobManifest(ctx context.Context, jobID, manifestID string) error {
	return s.db.WithContext(ctx).Model(&Job{}).Where("id = ?", jobID).Update("manifest_id", manifestID).Error
}

func (s *PostgresStore) CreateJobWithOutbox(ctx context.Context, job *Job, outbox *Outbox) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(job).Error; err != nil {
			return err
		}
		if err := tx.Create(outbox).Error; err != nil {
			return err
		}
		return nil
	})
}

func (s *PostgresStore) GetJob(ctx context.Context, id string) (*Job, error) {
	var job Job
	if err := s.db.WithContext(ctx).First(&job, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &job, nil
}

func (s *PostgresStore) UpdateJobStatus(ctx context.Context, id string, expectedStatus string, newStatus string) (bool, error) {
	res := s.db.WithContext(ctx).Model(&Job{}).Where("id = ? AND status = ?", id, expectedStatus).Update("status", newStatus)
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
}

func (s *PostgresStore) UpdateJobCost(ctx context.Context, id string, additionalCost float64) error {
	return s.db.WithContext(ctx).Model(&Job{}).Where("id = ?", id).Update("cost_usd", gorm.Expr("cost_usd + ?", additionalCost)).Error
}

func (s *PostgresStore) AppendEvent(ctx context.Context, event *Event) error {
	return s.db.WithContext(ctx).Create(event).Error
}

func (s *PostgresStore) SaveCheckpoint(ctx context.Context, checkpoint *Checkpoint) error {
	return s.db.WithContext(ctx).Create(checkpoint).Error
}

func (s *PostgresStore) GetSideEffect(ctx context.Context, id string) (*SideEffect, error) {
	var effect SideEffect
	if err := s.db.WithContext(ctx).First(&effect, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &effect, nil
}

func (s *PostgresStore) RecordSideEffect(ctx context.Context, effect *SideEffect) error {
	return s.db.WithContext(ctx).Create(effect).Error
}

func (s *PostgresStore) UpdateTaskLogs(ctx context.Context, id string, logs string) error {
	// Fallback implementation, normally handled on a different model
	return nil
}
