package store

import (
	"context"

	"github.com/pgvector/pgvector-go"
	"gorm.io/gorm/clause"
)

// UpsertJobLearning writes (or replaces) the single learning row for a job.
// There is one learning per job, keyed by job_id, so a re-index of the same job
// updates in place rather than accumulating duplicate rows. It never clobbers
// the terminal outcome/pr_url, which CompleteTask owns.
func (s *PostgresStore) UpsertJobLearning(ctx context.Context, learning *JobLearning) error {
	return s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "job_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"repo", "task", "summary", "embedding"}),
	}).Create(learning).Error
}

func (s *PostgresStore) GetJobLearnings(ctx context.Context, orgID string, jobIDs []string) ([]JobLearning, error) {
	var learnings []JobLearning
	if len(jobIDs) == 0 {
		return learnings, nil
	}
	if err := s.db.WithContext(ctx).Where("org_id = ? AND job_id IN ?", orgID, jobIDs).Find(&learnings).Error; err != nil {
		return nil, err
	}
	return learnings, nil
}

func (s *PostgresStore) SearchJobLearnings(ctx context.Context, orgID string, taskEmbedding []float32, limit int, excludeJobID string) ([]JobLearning, error) {
	var learnings []JobLearning

	query := s.db.WithContext(ctx).Where("org_id = ?", orgID)
	if excludeJobID != "" {
		query = query.Where("job_id != ?", excludeJobID)
	}

	vec := pgvector.NewVector(taskEmbedding)
	if err := query.Order(clause.Expr{SQL: "embedding <=> ?", Vars: []interface{}{vec}}).Limit(limit).Find(&learnings).Error; err != nil {
		return nil, err
	}
	return learnings, nil
}
