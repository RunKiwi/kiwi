package provider

import (
	"context"
	"testing"
)

// The mock embedder is what the planner tests run against, so its two
// guarantees — fixed 768-dim output and determinism per input — matter.
func TestMockEmbedderDeterministicAndSized(t *testing.T) {
	m := NewMockProvider()
	ctx := context.Background()

	a, err := m.Embed(ctx, "setup database")
	if err != nil {
		t.Fatalf("embed: %v", err)
	}
	if len(a) != 768 {
		t.Fatalf("dimension = %d, want 768", len(a))
	}

	b, _ := m.Embed(ctx, "setup database")
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("same input produced different vectors at %d: %v vs %v", i, a[i], b[i])
		}
	}

	c, _ := m.Embed(ctx, "a different task")
	same := true
	for i := range a {
		if a[i] != c[i] {
			same = false
			break
		}
	}
	if same {
		t.Fatalf("different inputs produced identical vectors")
	}
}
