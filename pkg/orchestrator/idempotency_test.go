package orchestrator

import (
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/ibreakthecloud/kiwi/pkg/auth"
	"gorm.io/gorm"
)

func newTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := auth.InitAuthDB(db); err != nil {
		t.Fatalf("migrate auth: %v", err)
	}
	if err := db.AutoMigrate(&TaskState{}, &TaskEvent{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	// clean slate for the shared in-memory db
	db.Exec("DELETE FROM task_states")
	return db
}

func TestFindByIdempotencyKey(t *testing.T) {
	db := newTestDB(t)
	db.Create(&TaskState{ID: "t1", IdempotencyKey: "key-abc", Status: "RUNNING", OrgID: "org1"})

	got, ok := findByIdempotencyKey(db, "key-abc", "org1")
	if !ok || got.ID != "t1" {
		t.Fatalf("hit: ok=%v got=%+v", ok, got)
	}
	// Same key, different org → should not match (cross-tenant isolation).
	if _, ok := findByIdempotencyKey(db, "key-abc", "org2"); ok {
		t.Errorf("cross-org idempotency key should not match")
	}
	if _, ok := findByIdempotencyKey(db, "nope", "org1"); ok {
		t.Errorf("miss should be false")
	}
	if _, ok := findByIdempotencyKey(db, "", "org1"); ok {
		t.Errorf("empty key should never match")
	}
}
