package store

import (
	"context"
	"testing"
)

func TestOrganization_CanRun(t *testing.T) {
	org := &Organization{ActivationState: "active"}
	if !org.CanRun() {
		t.Errorf("expected CanRun to be true for active org")
	}

	org.ActivationState = "inactive"
	if org.CanRun() {
		t.Errorf("expected CanRun to be false for inactive org")
	}
}

func TestOrganization_Defaults(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	org := Organization{ID: "org-def", Name: "Default Org"}
	if err := s.DB().Create(&org).Error; err != nil {
		t.Fatalf("failed to create org: %v", err)
	}

	fetched, err := s.GetOrganization(ctx, "org-def")
	if err != nil {
		t.Fatalf("failed to fetch org: %v", err)
	}

	if fetched.Type != "personal" {
		t.Errorf("expected type 'personal', got %q", fetched.Type)
	}
	if fetched.Plan != "free" {
		t.Errorf("expected plan 'free', got %q", fetched.Plan)
	}
	if fetched.ActivationState != "inactive" {
		t.Errorf("expected state 'inactive', got %q", fetched.ActivationState)
	}
	if fetched.DomainJoin != false {
		t.Errorf("expected domain_join false, got %v", fetched.DomainJoin)
	}
}
