package provider

import (
	"context"
	"testing"
)

func TestMockCriticAlwaysApproves(t *testing.T) {
	c := NewMockCritic()
	v, err := c.ReviewEdit(context.Background(), "task", "f.go", "old", "new", "boom")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !v.Approved {
		t.Fatalf("expected mock critic to approve, got %+v", v)
	}
}

// Compile-time guarantee the mock satisfies the interface.
var _ Critic = (*MockCritic)(nil)
