package store

import (
	"context"
	"testing"
)

func TestModelSourceDefaultsToKiwi(t *testing.T) {
	s := newTestStore(t)
	src, err := s.ModelSource(context.Background(), "org-that-does-not-exist")
	if err != nil {
		t.Fatal(err)
	}
	if src != ModelSourceKiwi {
		t.Errorf("source = %q, want %q", src, ModelSourceKiwi)
	}
}

func TestSetModelSourceRoundTrips(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if err := s.db.Create(&Organization{ID: "org1", Name: "acme"}).Error; err != nil {
		t.Fatal(err)
	}

	for _, src := range []string{ModelSourceBYOK, ModelSourceKiwi} {
		if err := s.SetModelSource(ctx, "org1", src); err != nil {
			t.Fatalf("set %q: %v", src, err)
		}
		got, err := s.ModelSource(ctx, "org1")
		if err != nil {
			t.Fatal(err)
		}
		if got != src {
			t.Errorf("source = %q, want %q", got, src)
		}
	}
}

func TestSetModelSourceRejectsUnknownValues(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if err := s.db.Create(&Organization{ID: "org1", Name: "acme"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.SetModelSource(ctx, "org1", "anthropic-only-please"); err == nil {
		t.Error("expected an error for an unknown source")
	}
}

func TestSetModelSourceRejectsAnUnknownOrg(t *testing.T) {
	s := newTestStore(t)
	if err := s.SetModelSource(context.Background(), "nope", ModelSourceKiwi); err == nil {
		t.Error("expected an error for an unknown org")
	}
}
