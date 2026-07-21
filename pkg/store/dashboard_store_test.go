package store

import (
	"context"
	"testing"
)

func TestFleetsAndModelsAreOrgScoped(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	if _, err := s.CreateFleet(ctx, "orgA", "prod", FleetBYOC); err != nil {
		t.Fatalf("CreateFleet: %v", err)
	}
	if _, err := s.CreateFleet(ctx, "orgB", "other", FleetManaged); err != nil {
		t.Fatalf("CreateFleet B: %v", err)
	}
	fa, err := s.ListFleets(ctx, "orgA")
	if err != nil {
		t.Fatalf("ListFleets: %v", err)
	}
	if len(fa) != 1 || fa[0].Name != "prod" || fa[0].Type != FleetBYOC {
		t.Errorf("orgA should see only its fleet, got %+v", fa)
	}

	// Invalid type falls back to managed.
	f, err := s.CreateFleet(ctx, "orgA", "bad-type", "bogus")
	if f.Type != FleetManaged {
		t.Errorf("invalid fleet type should default to managed, got %q", f.Type)
	}

	m, err := s.CreateModel(ctx, "orgA", "gemini-2.0-flash", "gemini")
	if err != nil {
		t.Fatalf("CreateModel: %v", err)
	}
	if _, err := s.CreateModel(ctx, "orgB", "claude-opus-4-8", "anthropic"); err != nil {
		t.Fatalf("CreateModel B: %v", err)
	}
	ma, _ := s.ListModels(ctx, "orgA")
	if len(ma) != 1 || ma[0].Name != "gemini-2.0-flash" {
		t.Errorf("orgA should see only its model, got %+v", ma)
	}

	// Cross-org delete must not remove another org's model.
	if err := s.DeleteModel(ctx, "orgB", m.ID); err != nil {
		t.Fatalf("DeleteModel: %v", err)
	}
	if ma, _ := s.ListModels(ctx, "orgA"); len(ma) != 1 {
		t.Errorf("orgB must not delete orgA's model; got %d", len(ma))
	}
	// Owner delete works.
	if err := s.DeleteModel(ctx, "orgA", m.ID); err != nil {
		t.Fatalf("DeleteModel owner: %v", err)
	}
	if ma, _ := s.ListModels(ctx, "orgA"); len(ma) != 0 {
		t.Errorf("owner delete should remove the model; got %d", len(ma))
	}
}
