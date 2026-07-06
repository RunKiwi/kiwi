package orchestrator

import (
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func newTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&TaskState{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	// clean slate for the shared in-memory db
	db.Exec("DELETE FROM task_states")
	return db
}

func TestFindByIdempotencyKey(t *testing.T) {
	db := newTestDB(t)
	db.Create(&TaskState{ID: "t1", IdempotencyKey: "key-abc", Status: "RUNNING"})

	got, ok := findByIdempotencyKey(db, "key-abc")
	if !ok || got.ID != "t1" {
		t.Fatalf("hit: ok=%v got=%+v", ok, got)
	}
	if _, ok := findByIdempotencyKey(db, "nope"); ok {
		t.Errorf("miss should be false")
	}
	if _, ok := findByIdempotencyKey(db, ""); ok {
		t.Errorf("empty key should never match")
	}
}
