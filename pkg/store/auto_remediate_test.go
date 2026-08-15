package store

import (
	"context"
	"testing"
)

func TestAutoRemediateDefaultsFalse(t *testing.T) {
	s := newTestStore(t)
	if err := s.DB().Create(&Organization{ID: "org1", Name: "acme"}).Error; err != nil {
		t.Fatal(err)
	}

	on, err := s.AutoRemediate(context.Background(), "org1")
	if err != nil {
		t.Fatal(err)
	}
	if on {
		t.Errorf("AutoRemediate = true, want false by default")
	}
}

func TestSetAutoRemediateRoundTrips(t *testing.T) {
	s := newTestStore(t)
	if err := s.DB().Create(&Organization{ID: "org1", Name: "acme"}).Error; err != nil {
		t.Fatal(err)
	}

	if err := s.SetAutoRemediate(context.Background(), "org1", true); err != nil {
		t.Fatal(err)
	}
	on, err := s.AutoRemediate(context.Background(), "org1")
	if err != nil {
		t.Fatal(err)
	}
	if !on {
		t.Errorf("AutoRemediate = false after SetAutoRemediate(true)")
	}
}

func TestAutoRemediateUnknownOrgIsFalse(t *testing.T) {
	s := newTestStore(t)
	on, err := s.AutoRemediate(context.Background(), "no-such-org")
	if err != nil {
		t.Fatal(err)
	}
	if on {
		t.Errorf("AutoRemediate = true for an unknown org, want false (fail closed)")
	}
}
