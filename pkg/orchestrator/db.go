package orchestrator

import (
	"fmt"

	"github.com/ibreakthecloud/kiwi/pkg/agentapi"
	"github.com/ibreakthecloud/kiwi/pkg/audit"
	"github.com/ibreakthecloud/kiwi/pkg/auth"
	"github.com/ibreakthecloud/kiwi/pkg/store"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// InitDB initializes GORM with Postgres and runs migrations for all
// models including auth tables (Organization, User, APIKey) and TaskState.
func InitDB(dsn string) (*gorm.DB, error) {
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Info),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Migrate auth tables (Organization, User, APIKey)
	if err := auth.InitAuthDB(db); err != nil {
		return nil, fmt.Errorf("failed to migrate auth schema: %w", err)
	}

	// Migrate TaskState, AuditLog, and TaskEvent schema
	if err := db.AutoMigrate(&TaskState{}, &audit.AuditLog{}, &TaskEvent{}, &store.Job{}, &store.Outbox{}, &store.Workflow{}, &store.Manifest{}); err != nil {
		return nil, fmt.Errorf("failed to migrate task schema: %w", err)
	}

	// Migrate the V2 control-plane schema (jobs, event log, checkpoints,
	// side-effect ledger, agents). This is the quick-validation path so the
	// tables exist without running migrations/0001 by hand; migrations/0001
	// remains the production source of truth (incl. FK constraints).
	if err := db.AutoMigrate(
		&store.Organization{}, &store.OrgLimits{}, &store.Job{}, &store.Outbox{},
		&store.Event{}, &store.Checkpoint{}, &store.SideEffect{}, &store.Agent{},
		&agentapi.JobToken{},
	); err != nil {
		return nil, fmt.Errorf("failed to migrate v2 store schema: %w", err)
	}

	return db, nil
}
