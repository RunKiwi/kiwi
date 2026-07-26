package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
)

func recordFor(orgID, jobID, hash, prev string) *ExecutionRecord {
	return &ExecutionRecord{
		RecordID:       "ver_" + jobID,
		OrgID:          orgID,
		JobID:          jobID,
		Ver:            "kiwi.ver/v1",
		PrevRecordHash: prev,
		RecordHash:     hash,
		Body:           json.RawMessage(`{}`),
	}
}

func TestAppendExecutionRecordChainsInOrder(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	var hashes []string
	for i := 0; i < 3; i++ {
		jobID := fmt.Sprintf("job_%d", i)
		hash := fmt.Sprintf("sha256:%d", i)
		rec, err := s.AppendExecutionRecord(ctx, "org_1", jobID, func(prev string) (*ExecutionRecord, error) {
			return recordFor("org_1", jobID, hash, prev), nil
		})
		if err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
		hashes = append(hashes, rec.RecordHash)
	}

	// The genesis record links to nothing; each later one links to its predecessor.
	first, err := s.GetExecutionRecord(ctx, "org_1", "job_0")
	if err != nil {
		t.Fatal(err)
	}
	if first.PrevRecordHash != "" {
		t.Errorf("genesis prev = %q, want empty", first.PrevRecordHash)
	}
	third, err := s.GetExecutionRecord(ctx, "org_1", "job_2")
	if err != nil {
		t.Fatal(err)
	}
	if third.PrevRecordHash != hashes[1] {
		t.Errorf("prev = %q, want %q", third.PrevRecordHash, hashes[1])
	}

	head, err := s.GetExecutionRecordChainHead(ctx, "org_1")
	if err != nil {
		t.Fatal(err)
	}
	if head != hashes[2] {
		t.Errorf("head = %q, want %q", head, hashes[2])
	}
}

// The original implementation read the chain head *outside* the transaction and
// built the record against it, so two jobs finishing at once derived the same
// prev hash and one was silently dropped — a gap indistinguishable from
// deletion in a log whose whole purpose is to be tamper-evident.
//
// The fix is structural: build runs inside the transaction and is handed the
// head that was just locked, so it cannot observe a stale one. That is what
// this asserts. A genuine concurrent race needs Postgres row locks; SQLite
// serializes writers, so exercising it here would prove nothing.
func TestAppendExecutionRecordBuildsAgainstLockedHead(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	var observed []string
	for i := 0; i < 3; i++ {
		jobID := fmt.Sprintf("job_%d", i)
		hash := fmt.Sprintf("sha256:%d", i)
		if _, err := s.AppendExecutionRecord(ctx, "org_1", jobID, func(prev string) (*ExecutionRecord, error) {
			observed = append(observed, prev)
			return recordFor("org_1", jobID, hash, prev), nil
		}); err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
	}

	want := []string{"", "sha256:0", "sha256:1"}
	for i, w := range want {
		if observed[i] != w {
			t.Errorf("build %d saw prev %q, want %q", i, observed[i], w)
		}
	}
}

// A record that does not link to the head it was given must be refused rather
// than inserted, so the chain can never fork.
func TestAppendExecutionRecordRejectsBrokenLink(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	if _, err := s.AppendExecutionRecord(ctx, "org_1", "job_0", func(prev string) (*ExecutionRecord, error) {
		return recordFor("org_1", "job_0", "sha256:0", prev), nil
	}); err != nil {
		t.Fatal(err)
	}

	_, err := s.AppendExecutionRecord(ctx, "org_1", "job_1", func(prev string) (*ExecutionRecord, error) {
		// Ignore the locked head and claim a different predecessor.
		return recordFor("org_1", "job_1", "sha256:1", "sha256:forged"), nil
	})
	if err == nil {
		t.Fatal("a record linking to the wrong predecessor must be rejected")
	}
	if _, err := s.GetExecutionRecord(ctx, "org_1", "job_1"); err == nil {
		t.Fatal("the rejected record must not have been inserted")
	}
	// The head must be untouched by the failed append.
	head, err := s.GetExecutionRecordChainHead(ctx, "org_1")
	if err != nil {
		t.Fatal(err)
	}
	if head != "sha256:0" {
		t.Errorf("head = %q, want sha256:0", head)
	}
}

func TestAppendExecutionRecordIsIdempotentPerJob(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	build := func(prev string) (*ExecutionRecord, error) {
		return recordFor("org_1", "job_1", "sha256:a", prev), nil
	}
	if _, err := s.AppendExecutionRecord(ctx, "org_1", "job_1", build); err != nil {
		t.Fatal(err)
	}
	_, err := s.AppendExecutionRecord(ctx, "org_1", "job_1", build)
	if !errors.Is(err, ErrRecordExists) {
		t.Fatalf("second append err = %v, want ErrRecordExists", err)
	}
}

// A job ID from another tenant must not resolve, so the API cannot confirm that
// someone else's record exists.
func TestGetExecutionRecordIsOrgScoped(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	if _, err := s.AppendExecutionRecord(ctx, "org_1", "job_1", func(prev string) (*ExecutionRecord, error) {
		return recordFor("org_1", "job_1", "sha256:a", prev), nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetExecutionRecord(ctx, "org_2", "job_1"); err == nil {
		t.Fatal("another org's job ID must not resolve")
	}
}
