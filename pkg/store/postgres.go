package store

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrJobNotFound = errors.New("job not found")
var ErrPlanStatusConflict = errors.New("plan status transition conflict")

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

func (s *PostgresStore) SetJobPlanPendingReview(ctx context.Context, jobID, planMarkdown string) error {
	res := s.db.WithContext(ctx).Model(&Job{}).Where("id = ? AND plan_status = ''", jobID).Updates(map[string]interface{}{
		"plan_status":   "pending_review",
		"plan_markdown": planMarkdown,
		"status":        "PLAN_REVIEW",
		"updated_at":    time.Now(),
	})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrPlanStatusConflict
	}
	return nil
}

func (s *PostgresStore) ApproveJobPlan(ctx context.Context, jobID string) error {
	now := time.Now()
	res := s.db.WithContext(ctx).Model(&Job{}).Where("id = ? AND plan_status = ?", jobID, "pending_review").Updates(map[string]interface{}{
		"plan_status":      "approved",
		"plan_accepted_at": &now,
		"status":           "RUNNING",
		"updated_at":       now,
	})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrPlanStatusConflict
	}
	return nil
}

func (s *PostgresStore) ApproveJobPlanAndEnqueue(ctx context.Context, jobID string, continuation *QueuedTask) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		now := time.Now()
		res := tx.Model(&Job{}).Where("id = ? AND plan_status = ?", jobID, "pending_review").Updates(map[string]interface{}{
			"plan_status":      "approved",
			"plan_accepted_at": &now,
			"status":           "RUNNING",
			"updated_at":       now,
		})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return ErrPlanStatusConflict
		}
		if continuation.Status == "" {
			continuation.Status = TaskQueued
		}
		return tx.Create(continuation).Error
	})
}

func (s *PostgresStore) RejectJobPlanAndRequestRevision(ctx context.Context, jobID, reason string, continuation *QueuedTask) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		res := tx.Model(&Job{}).Where("id = ? AND plan_status = ?", jobID, "pending_review").Updates(map[string]interface{}{
			// Reset, not a terminal value: the daemon re-plans and reports
			// PLAN_REVIEW again, and SetJobPlanPendingReview only fires from "".
			"plan_status":          "",
			"plan_rejected_reason": reason,
			"status":               "RUNNING",
			"updated_at":           time.Now(),
		})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return ErrPlanStatusConflict
		}
		if continuation.Status == "" {
			continuation.Status = TaskQueued
		}
		return tx.Create(continuation).Error
	})
}

func (s *PostgresStore) SetJobSpendCap(ctx context.Context, orgID, jobID string, capUSD float64) error {
	res := s.db.WithContext(ctx).Model(&Job{}).
		Where("id = ? AND org_id = ?", jobID, orgID).
		Update("spend_cap_usd", capUSD)
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrJobNotFound
	}
	return nil
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
