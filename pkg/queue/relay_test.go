package queue

import (
	"context"
	"errors"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/ibreakthecloud/kiwi/pkg/store"
	"gorm.io/gorm"
)

type mockPublisher struct {
	publishFunc func(ctx context.Context, topic string, payload []byte, msgID string) error
	published   int
}

func (m *mockPublisher) Publish(ctx context.Context, topic string, payload []byte, msgID string) error {
	if m.publishFunc != nil {
		return m.publishFunc(ctx, topic, payload, msgID)
	}
	m.published++
	return nil
}

func setupDB(t *testing.T) *gorm.DB {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	db.AutoMigrate(&store.Job{}, &store.Outbox{})
	return db
}

func TestRelay_ProcessBatch(t *testing.T) {
	db := setupDB(t)
	s := store.NewPostgresStore(db)

	job := &store.Job{ID: "j1", OrgID: "org1", UserID: "u1"}
	outbox := &store.Outbox{JobID: "j1", Topic: "jobs.submitted", Payload: map[string]interface{}{"job_id": "j1"}}
	if err := s.CreateJobWithOutbox(context.Background(), job, outbox); err != nil {
		t.Fatalf("failed to insert: %v", err)
	}

	pub := &mockPublisher{}
	relay := NewRelay(s, pub)

	processed, err := relay.processBatch(context.Background())
	if err != nil {
		t.Fatalf("processBatch error: %v", err)
	}
	if processed != 1 {
		t.Errorf("expected 1 processed, got %d", processed)
	}
	if pub.published != 1 {
		t.Errorf("expected 1 published message, got %d", pub.published)
	}

	var ob store.Outbox
	if err := db.First(&ob).Error; err != nil {
		t.Fatalf("fetch outbox: %v", err)
	}
	if ob.PublishedAt == nil {
		t.Error("expected PublishedAt to be set")
	}

	processed2, err := relay.processBatch(context.Background())
	if err != nil {
		t.Fatalf("processBatch 2 error: %v", err)
	}
	if processed2 != 0 {
		t.Errorf("expected 0 processed, got %d", processed2)
	}
}

func TestRelay_CrashBetweenCommitAndPublish(t *testing.T) {
	db := setupDB(t)
	s := store.NewPostgresStore(db)

	job := &store.Job{ID: "j2", OrgID: "org1", UserID: "u1"}
	outbox := &store.Outbox{JobID: "j2", Topic: "jobs.submitted", Payload: map[string]interface{}{"job_id": "j2"}}
	s.CreateJobWithOutbox(context.Background(), job, outbox)

	pub := &mockPublisher{
		publishFunc: func(ctx context.Context, topic string, payload []byte, msgID string) error {
			return errors.New("simulated publish failure")
		},
	}
	relay := NewRelay(s, pub)

	_, err := relay.processBatch(context.Background())
	if err == nil {
		t.Fatal("expected error from processBatch")
	}

	var ob store.Outbox
	db.First(&ob)
	if ob.PublishedAt != nil {
		t.Error("expected PublishedAt to be nil on failure")
	}

	pub.publishFunc = nil
	processed, _ := relay.processBatch(context.Background())
	if processed != 1 {
		t.Errorf("expected 1 processed on retry, got %d", processed)
	}
}

func TestRelay_MalformedRow(t *testing.T) {
	db := setupDB(t)
	s := store.NewPostgresStore(db)

	job := &store.Job{ID: "j3", OrgID: "org1", UserID: "u1"}
	outbox := &store.Outbox{JobID: "j3", Topic: "jobs.submitted", Payload: map[string]interface{}{"wrong": "data"}}
	s.CreateJobWithOutbox(context.Background(), job, outbox)

	pub := &mockPublisher{}
	relay := NewRelay(s, pub)

	processed, err := relay.processBatch(context.Background())
	if err != nil {
		t.Fatalf("expected no error for malformed row skip, got %v", err)
	}
	if processed != 0 {
		t.Errorf("expected 0 processed, got %d", processed)
	}
	if pub.published != 0 {
		t.Errorf("expected 0 published, got %d", pub.published)
	}

	var ob store.Outbox
	db.First(&ob)
	if ob.PublishedAt == nil {
		t.Error("expected PublishedAt to be set to skip malformed row")
	}
}
