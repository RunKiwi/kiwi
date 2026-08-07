package store

import (
	"context"
	"testing"
)

// Every ambiguous case must deny. This function decides whether Kiwi's own API
// key is handed to a machine, so anything short of proof that Kiwi operates
// that machine is a no.
func TestIsManagedFleetFailsClosed(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	managed, err := s.CreateFleet(ctx, "o1", "kiwi-managed", FleetManaged)
	if err != nil {
		t.Fatalf("create managed fleet: %v", err)
	}
	byoc, err := s.CreateFleet(ctx, "o1", "customer-cloud", FleetBYOC)
	if err != nil {
		t.Fatalf("create byoc fleet: %v", err)
	}

	cases := []struct {
		name    string
		fleetID string
		want    bool
	}{
		{"managed fleet", managed.ID, true},
		// The shared free fleet is a well-known id that may have no fleets row
		// at all, so it is recognised by name before the table is consulted.
		{"shared free fleet by name", SharedFreeFleet, true},
		{"byoc fleet", byoc.ID, false},
		// "Belongs to no specific fleet" is not "Kiwi operates it".
		{"empty fleet id", "", false},
		// A dangling reference proves nothing about who runs the machine.
		{"fleet id with no row", "flt_does_not_exist", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := s.IsManagedFleet(ctx, tc.fleetID)
			if err != nil {
				t.Fatalf("IsManagedFleet(%q): %v", tc.fleetID, err)
			}
			if got != tc.want {
				t.Errorf("IsManagedFleet(%q) = %v, want %v", tc.fleetID, got, tc.want)
			}
		})
	}
}

// The constant must be one definition. Two copies that drift would let a
// free-fleet daemon fail the check and lose its platform key, or worse.
func TestSharedFreeFleetConstant(t *testing.T) {
	if SharedFreeFleet != "shared-free" {
		t.Errorf("SharedFreeFleet = %q, want shared-free", SharedFreeFleet)
	}
}
