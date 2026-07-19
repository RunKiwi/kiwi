package orchestrator

import (
	"fmt"

	"os"

	"strconv"
	"time"

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

	if os.Getenv("KIWI_AUTOMIGRATE") == "true" {
		// Migrate auth tables (Organization, User, APIKey)
		if err := auth.InitAuthDB(db); err != nil {
			return nil, fmt.Errorf("failed to migrate auth schema: %w", err)
		}

		// Migrate TaskState, AuditLog, and TaskEvent schema
		if err := db.AutoMigrate(&TaskState{}, &audit.AuditLog{}, &TaskEvent{}, &store.Job{}, &store.Outbox{}, &store.Workflow{}, &store.Manifest{}); err != nil {
			return nil, fmt.Errorf("failed to migrate task schema: %w", err)
		}

		if err := db.AutoMigrate(
			&store.Organization{}, &store.OrgLimits{}, &store.Job{}, &store.Outbox{},
			&store.Event{}, &store.Checkpoint{}, &store.SideEffect{}, &store.Agent{},
			&store.QueuedTask{}, &store.Credential{},
			&store.Daemon{}, &store.DaemonJoinToken{},
			&store.PlanSubmission{},
			&store.Fleet{}, &store.ModelEntry{},
			&agentapi.JobToken{},
		); err != nil {
			return nil, fmt.Errorf("failed to migrate v2 store schema: %w", err)
		}
	} else if os.Getenv("KIWI_SKIP_BOOT_MIGRATE") != "true" {
		// Apply SQL migrations
		if err := RunMigrations(db); err != nil {
			return nil, fmt.Errorf("failed to run migrations: %w", err)
		}
	}

	sqlDB, err := db.DB()
	if err == nil {
		if maxOpen := os.Getenv("KIWI_DB_MAX_OPEN"); maxOpen != "" {
			if v, err := strconv.Atoi(maxOpen); err == nil {
				sqlDB.SetMaxOpenConns(v)
			}
		}
		if maxIdle := os.Getenv("KIWI_DB_MAX_IDLE"); maxIdle != "" {
			if v, err := strconv.Atoi(maxIdle); err == nil {
				sqlDB.SetMaxIdleConns(v)
			}
		}
		if maxLife := os.Getenv("KIWI_DB_CONN_MAX_LIFETIME"); maxLife != "" {
			if v, err := time.ParseDuration(maxLife); err == nil {
				sqlDB.SetConnMaxLifetime(v)
			}
		}
	}

	// Ensure system organization exists for bootstrap admin token
	sysOrg := &store.Organization{
		ID:   "system",
		Name: "System",
	}
	db.Where(store.Organization{ID: "system"}).Assign(store.Organization{Name: "System"}).FirstOrCreate(sysOrg)

	return db, nil
}
