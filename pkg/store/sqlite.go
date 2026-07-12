package store

import (
	"gorm.io/gorm"
)

// SQLiteStore wraps PostgresStore because they both use GORM
// under the hood, and they share identical ORM methods for now.
// It is kept separate to allow for SQLite-specific dialect optimizations later.
type SQLiteStore struct {
	*PostgresStore
}

func NewSQLiteStore(db *gorm.DB) *SQLiteStore {
	return &SQLiteStore{
		PostgresStore: NewPostgresStore(db),
	}
}
