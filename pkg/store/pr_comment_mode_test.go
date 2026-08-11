package store

import (
	"context"
	"testing"
)

// An org that has never touched the setting gets mention, not any. The
// difference is a user's free allowance: on "any", "thanks, looks good"
// becomes a billable round.
func TestPRCommentModeDefaultsToMention(t *testing.T) {
	s := newTestStore(t)
	mode, err := s.PRCommentMode(context.Background(), "org-that-does-not-exist")
	if err != nil {
		t.Fatal(err)
	}
	if mode != PRCommentModeMention {
		t.Errorf("mode = %q, want %q", mode, PRCommentModeMention)
	}
}

func TestSetPRCommentModeRoundTrips(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if err := s.db.Create(&Organization{ID: "org1", Name: "acme"}).Error; err != nil {
		t.Fatal(err)
	}

	for _, mode := range []string{PRCommentModeAny, PRCommentModeOff, PRCommentModeMention} {
		if err := s.SetPRCommentMode(ctx, "org1", mode); err != nil {
			t.Fatalf("set %q: %v", mode, err)
		}
		got, err := s.PRCommentMode(ctx, "org1")
		if err != nil {
			t.Fatal(err)
		}
		if got != mode {
			t.Errorf("mode = %q, want %q", got, mode)
		}
	}
}

// A typo must not quietly disable the feature, and must not quietly enable the
// expensive one.
func TestSetPRCommentModeRejectsUnknownModes(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if err := s.db.Create(&Organization{ID: "org1", Name: "acme"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.SetPRCommentMode(ctx, "org1", "sometimes"); err == nil {
		t.Error("expected an error for an unknown mode")
	}
}

// Setting a mode for an org that does not exist is a bug in the caller, not a
// row to create: it would leave an organisation with no name and no plan.
func TestSetPRCommentModeRejectsAnUnknownOrg(t *testing.T) {
	s := newTestStore(t)
	if err := s.SetPRCommentMode(context.Background(), "nope", PRCommentModeAny); err == nil {
		t.Error("expected an error for an unknown org")
	}
}
