package orchestrator

import (
	"fmt"

	"github.com/glebarez/sqlite"
	"github.com/ibreakthecloud/kiwi/pkg/audit"
	"github.com/ibreakthecloud/kiwi/pkg/auth"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// InitDB initializes GORM with pure-Go SQLite and runs migrations for all
// models including auth tables (Organization, User, APIKey) and TaskState.
func InitDB(dbPath string) (*gorm.DB, error) {
	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{
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
	if err := db.AutoMigrate(&TaskState{}, &audit.AuditLog{}, &TaskEvent{}); err != nil {
		return nil, fmt.Errorf("failed to migrate task schema: %w", err)
	}

	return db, nil
}
