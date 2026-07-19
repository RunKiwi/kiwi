package provider

import (
	"context"
	"testing"
)

func TestMockProviderComplete(t *testing.T) {
	mp := NewMockProvider()

	// Default behavior
	res, err := mp.Complete(context.Background(), "sys", "user")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res != "" {
		t.Errorf("expected empty string, got %q", res)
	}

	// Custom behavior
	mp.CompleteFunc = func(sys, usr string) (string, error) {
		return "hello " + usr, nil
	}
	res, err = mp.Complete(context.Background(), "sys", "world")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res != "hello world" {
		t.Errorf("expected 'hello world', got %q", res)
	}
}
