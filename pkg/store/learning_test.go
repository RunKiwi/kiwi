package store_test

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/ibreakthecloud/kiwi/pkg/store"
	"github.com/pgvector/pgvector-go"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func mockEmbed(text string) []float32 {
	hash := sha256.Sum256([]byte(text))
	vec := make([]float32, 768)
	for i := 0; i < 768; i++ {
		idx := (i * 2) % 31
		val := binary.BigEndian.Uint16(hash[idx : idx+2])
		vec[i] = float32(val) / 65535.0
	}
	return vec
}

func TestJobLearning(t *testing.T) {
	// Need a real postgres instance with pgvector for testing pgvector stuff.
	// Since tests might run in CI without pgvector or we might mock it, we'll try to connect
	// and skip if unavailable.
	dsn := "host=localhost user=postgres password=postgres dbname=kiwi port=5432 sslmode=disable"
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Skip("Skipping test, no postgres available")
	}

	// Make sure extension exists
	db.Exec("CREATE EXTENSION IF NOT EXISTS vector")
	db.AutoMigrate(&store.JobLearning{})

	// Clean up table before tests
	db.Exec("DELETE FROM job_learnings")

	s := store.NewPostgresStore(db)
	ctx := context.Background()

	orgID1 := "org_" + uuid.NewString()
	orgID2 := "org_" + uuid.NewString()

	// 1. Create jobs
	vec1 := mockEmbed("setup database")
	jl1 := &store.JobLearning{
		ID:        uuid.NewString(),
		JobID:     "job1",
		OrgID:     orgID1,
		Repo:      "test/repo",
		Task:      "setup database",
		Summary:   "Created db setup script",
		Embedding: func() *pgvector.Vector { v := pgvector.NewVector(vec1); return &v }(),
		CreatedAt: time.Now(),
	}
	if err := s.UpsertJobLearning(ctx, jl1); err != nil {
		t.Fatalf("failed to upsert: %v", err)
	}

	vec2 := mockEmbed("setup database but differently")
	jl2 := &store.JobLearning{
		ID:        uuid.NewString(),
		JobID:     "job2",
		OrgID:     orgID2, // Different org
		Repo:      "test/repo",
		Task:      "setup database",
		Summary:   "Created db setup script 2",
		Embedding: func() *pgvector.Vector { v := pgvector.NewVector(vec2); return &v }(),
		CreatedAt: time.Now(),
	}
	if err := s.UpsertJobLearning(ctx, jl2); err != nil {
		t.Fatalf("failed to upsert: %v", err)
	}

	// 2. GetJobLearnings org scoping
	learnings, err := s.GetJobLearnings(ctx, orgID1, []string{"job1", "job2"})
	if err != nil {
		t.Fatalf("failed GetJobLearnings: %v", err)
	}
	if len(learnings) != 1 || learnings[0].JobID != "job1" {
		t.Fatalf("GetJobLearnings returned wrong org scoping: expected 1 got %d", len(learnings))
	}

	// 3. Re-upserting the same job (different PK, same job_id) updates in place
	// rather than creating a duplicate row — one learning per job, keyed on job_id.
	jl1b := *jl1
	jl1b.ID = uuid.NewString()
	jl1b.Summary = "updated summary"
	if err := s.UpsertJobLearning(ctx, &jl1b); err != nil {
		t.Fatalf("re-upsert: %v", err)
	}
	learnings, _ = s.GetJobLearnings(ctx, orgID1, []string{"job1"})
	if len(learnings) != 1 {
		t.Fatalf("expected 1 row after re-upsert (conflict on job_id), got %d", len(learnings))
	}
	if learnings[0].Summary != "updated summary" {
		t.Fatalf("re-upsert didn't update summary in place: %q", learnings[0].Summary)
	}

	// 4. Search
	// Add one more in org 1
	vec3 := mockEmbed("unrelated task")
	jl3 := &store.JobLearning{
		ID:        uuid.NewString(),
		JobID:     "job3",
		OrgID:     orgID1,
		Repo:      "test/repo",
		Task:      "unrelated task",
		Summary:   "Did nothing",
		Embedding: func() *pgvector.Vector { v := pgvector.NewVector(vec3); return &v }(),
		CreatedAt: time.Now(),
	}
	s.UpsertJobLearning(ctx, jl3)

	// Search for 'setup database'
	searchVec := mockEmbed("setup database")
	res, err := s.SearchJobLearnings(ctx, orgID1, searchVec, 3, "job_new")
	if err != nil {
		t.Fatalf("failed search: %v", err)
	}
	if len(res) != 2 {
		t.Fatalf("expected 2 results, got %d", len(res))
	}
	// Nearest should be job1
	if res[0].JobID != "job1" {
		t.Fatalf("expected nearest neighbor to be job1, got %s", res[0].JobID)
	}

	// Test exclude job ID
	res, err = s.SearchJobLearnings(ctx, orgID1, searchVec, 3, "job1")
	if err != nil {
		t.Fatalf("failed search: %v", err)
	}
	if len(res) != 1 || res[0].JobID != "job3" {
		t.Fatalf("expected 1 result (job3) when excluding job1, got %d", len(res))
	}
}
