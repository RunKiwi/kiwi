package store

import (
	"context"
	"testing"
)

// This function decides whether Kiwi's own API key is handed to a machine, so
// anything short of proof that Kiwi operates that machine must deny.
//
// The case that matters most is the managed-typed fleet: every org gets one at
// signup (auth.CreateDefaultFleet), and CreateFleet takes the type from a
// request body. If Fleet.Type were trusted here, any signed-up user could mint
// a join token against their own "managed" fleet, run kiwidaemon anywhere, and
// receive Kiwi's platform credential sealed to a key they hold.
func TestIsKiwiOperatedFleetTrustsOnlyTheSharedFreeFleet(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	managed, err := s.CreateFleet(ctx, "o1", "Managed (Default)", FleetManaged)
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
		// The provisioner launches these daemons itself; the id is a constant
		// with no fleets row, so it cannot be claimed through the API.
		{"shared free fleet", SharedFreeFleet, true},

		// A customer-owned row labelled "managed" is NOT proof of anything.
		{"customer-created managed fleet", managed.ID, false},
		{"byoc fleet", byoc.ID, false},
		// "Belongs to no specific fleet" is not "Kiwi operates it".
		{"empty fleet id", "", false},
		{"fleet id with no row", "flt_does_not_exist", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := s.IsKiwiOperatedFleet(ctx, "o1", tc.fleetID)
			if err != nil {
				t.Fatalf("IsKiwiOperatedFleet(%q): %v", tc.fleetID, err)
			}
			if got != tc.want {
				t.Errorf("IsKiwiOperatedFleet(%q) = %v, want %v", tc.fleetID, got, tc.want)
			}
		})
	}
}

// A misspelled or absent type must not mint a privileged fleet.
func TestCreateFleetDefaultsToBYOC(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	for _, ftype := range []string{"", "Managed", "manged", "whatever"} {
		f, err := s.CreateFleet(ctx, "o1", "f", ftype)
		if err != nil {
			t.Fatalf("CreateFleet(%q): %v", ftype, err)
		}
		if f.Type != FleetBYOC {
			t.Errorf("CreateFleet(%q).Type = %q, want %q", ftype, f.Type, FleetBYOC)
		}
	}

	// The two recognised values still round-trip.
	for _, ftype := range []string{FleetManaged, FleetBYOC} {
		f, err := s.CreateFleet(ctx, "o1", "f", ftype)
		if err != nil {
			t.Fatalf("CreateFleet(%q): %v", ftype, err)
		}
		if f.Type != ftype {
			t.Errorf("CreateFleet(%q).Type = %q", ftype, f.Type)
		}
	}
}

func TestSharedFreeFleetConstant(t *testing.T) {
	if SharedFreeFleet != "shared-free" {
		t.Errorf("SharedFreeFleet = %q, want shared-free", SharedFreeFleet)
	}
}
