package orchestrator

import (
	"os"
	"testing"

	"github.com/ibreakthecloud/kiwi/pkg/auth"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestRunMigrations(t *testing.T) {
	dsn := os.Getenv("KIWI_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("Skipping migration test; KIWI_TEST_PG_DSN not set")
	}

	// Connect to postgres to create a temporary database for testing
	baseDB, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("Failed to connect to base postgres: %v", err)
	}

	dbName := "kiwi_test_migrations"

	// Drop DB if it exists, ignoring errors
	baseDB.Exec("DROP DATABASE " + dbName)

	if err := baseDB.Exec("CREATE DATABASE " + dbName).Error; err != nil {
		t.Fatalf("Failed to create test DB: %v", err)
	}
	defer baseDB.Exec("DROP DATABASE " + dbName) // cleanup

	// Connect to the new test DB
	testDSN := dsn + " dbname=" + dbName
	db, err := gorm.Open(postgres.Open(testDSN), &gorm.Config{})
	if err != nil {
		t.Fatalf("Failed to connect to test DB: %v", err)
	}

	// Run migrations
	if err := RunMigrations(db); err != nil {
		t.Fatalf("RunMigrations failed: %v", err)
	}

	// Re-running should be a no-op and not fail
	if err := RunMigrations(db); err != nil {
		t.Fatalf("RunMigrations (idempotent run) failed: %v", err)
	}

	// Verify that a newly added table exists and matches a model
	// E.g., we can verify we can query `users`
	var count int64
	if err := db.Model(&auth.User{}).Count(&count).Error; err != nil {
		t.Errorf("Failed to query users table: %v", err)
	}
}
