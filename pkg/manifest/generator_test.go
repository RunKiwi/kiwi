package manifest

import (
	"testing"

	"github.com/ibreakthecloud/kiwi/pkg/store"
)

func TestGenerateDeterminism(t *testing.T) {
	job1 := &store.Job{
		ID:    "job1",
		OrgID: "org1",
		Inputs: map[string]interface{}{
			"foo": "bar",
			"baz": 123,
		},
	}

	job2 := &store.Job{
		ID:    "job2", // different job ID
		OrgID: "org1",
		Inputs: map[string]interface{}{
			"baz": 123,
			"foo": "bar", // same inputs, different map iteration order (go maps)
		},
	}

	// Note: go's standard encoding/json marshals map keys in sorted order,
	// so the resulting JSON string and hash should be perfectly identical.
	m1, err := Generate(job1, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	m2, err := Generate(job2, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if m1.ID != m2.ID {
		t.Errorf("Expected deterministic hashes, got %s and %s", m1.ID, m2.ID)
	}
}

func TestGenerateNestedDeterminism(t *testing.T) {
	job1 := &store.Job{
		ID:    "job1",
		OrgID: "org1",
		Inputs: map[string]interface{}{
			"config": map[string]interface{}{
				"a": 1,
				"b": 2,
			},
		},
	}

	job2 := &store.Job{
		ID:    "job2",
		OrgID: "org1",
		Inputs: map[string]interface{}{
			"config": map[string]interface{}{
				"b": 2,
				"a": 1,
			},
		},
	}

	m1, err := Generate(job1, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	m2, err := Generate(job2, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if m1.ID != m2.ID {
		t.Errorf("Expected deterministic hashes for nested maps, got %s and %s", m1.ID, m2.ID)
	}
}
