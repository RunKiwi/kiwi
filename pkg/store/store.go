package store

import (
	"context"

	"gorm.io/gorm"
)

// Store defines the data access interface for the control plane.
// It abstracts away the underlying database (e.g. Postgres or SQLite)
// and provides a unified interface for all subsystems.
type Store interface {
	// Tenancy & Limits
	GetOrganization(ctx context.Context, id string) (*Organization, error)
	GetOrgLimits(ctx context.Context, orgID string) (*OrgLimits, error)
	// Jobs (Target V2 Schema)
	CreateJobWithOutbox(ctx context.Context, job *Job, outbox *Outbox) error
	GetJob(ctx context.Context, id string) (*Job, error)
	UpdateJobStatus(ctx context.Context, id string, expectedStatus string, newStatus string) (bool, error)
	UpdateJobCost(ctx context.Context, id string, additionalCost float64) error

	// Events & Checkpoints
	AppendEvent(ctx context.Context, event *Event) error
	SaveCheckpoint(ctx context.Context, checkpoint *Checkpoint) error

	// Side Effects (Idempotency)
	GetSideEffect(ctx context.Context, id string) (*SideEffect, error)
	RecordSideEffect(ctx context.Context, effect *SideEffect) error

	// Legacy orchestrator tasks mapping (temp for V1-V2 transition)
	UpdateTaskLogs(ctx context.Context, id string, logs string) error

	// DB Accessor for gradual migration of legacy endpoints
	DB() *gorm.DB
}
