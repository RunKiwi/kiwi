package store

import (
	"time"

	"github.com/pgvector/pgvector-go"
)

type JobLearning struct {
	ID        string           `gorm:"primaryKey" json:"id"`
	JobID     string           `gorm:"uniqueIndex;not null" json:"job_id"`
	OrgID     string           `gorm:"index;not null" json:"org_id"`
	Repo      string           `gorm:"not null" json:"repo"`
	Task      string           `gorm:"not null" json:"task"`
	Summary   string           `gorm:"not null" json:"summary"`
	PRURL     *string          `json:"pr_url"`
	Outcome   *string          `json:"outcome"`
	Embedding *pgvector.Vector `gorm:"type:vector(768)" json:"embedding"`
	CreatedAt time.Time        `gorm:"not null;default:current_timestamp" json:"created_at"`
}

func (JobLearning) TableName() string { return "job_learnings" }
