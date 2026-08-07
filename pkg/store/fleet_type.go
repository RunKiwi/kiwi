package store

import "context"

// SharedFreeFleet is the well-known fleet id every free-tier daemon joins.
//
// It lives here rather than in pkg/auth because pkg/auth imports pkg/store, so
// this is the lower of the two and can be the single definition. auth's
// SharedFreeFleet aliases this one.
const SharedFreeFleet = "shared-free"

// IsKiwiOperatedFleet reports whether a fleet runs on hardware Kiwi itself
// operates. It gates whether Kiwi's own API key is sealed to a daemon.
//
// It deliberately does NOT consult Fleet.Type. `Type == "managed"` is a label a
// customer can write: auth.CreateDefaultFleet gives every org a managed-typed
// fleet at signup, and CreateFleet accepts the type from the request body. A
// customer could therefore mint a join token against their own "managed" fleet,
// run kiwidaemon on their laptop, and be handed Kiwi's platform credential —
// sealed to an X25519 key they generated and hold the private half of. Type
// describes what a fleet is *for*; it is not evidence of who runs the machine.
//
// The only fleet Kiwi provably operates today is the shared free fleet, whose
// daemons the provisioner launches itself (pkg/provisioner). Its id is a
// compile-time constant with no row in the fleets table, so it cannot be
// created, renamed, or claimed through the API — CreateDaemonJoinToken's
// ownership check rejects a fleet id the org has no row for.
//
// When managed-dedicated ships, it must be recognised here by a column the
// provisioner sets when it launches the container — never by a type the
// customer supplies. Widening this function to trust Fleet.Type would reopen
// the exfiltration path described above.
func (s *PostgresStore) IsKiwiOperatedFleet(ctx context.Context, orgID, fleetID string) (bool, error) {
	return fleetID == SharedFreeFleet, nil
}
