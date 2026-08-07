package store

import "context"

// SharedFreeFleet is the well-known fleet id every free-tier daemon joins.
//
// It lives here rather than in pkg/auth because pkg/auth imports pkg/store, so
// this is the lower of the two and can be the single definition. auth's
// SharedFreeFleet aliases this one.
const SharedFreeFleet = "shared-free"

// IsManagedFleet reports whether a fleet is one Kiwi operates.
//
// This gates whether Kiwi's own API key is sealed to a daemon, so it fails
// closed: only a positive proof of Kiwi operation returns true.
//
//   - The shared free fleet is recognised by name, because it is a well-known
//     constant that may have no row in the fleets table at all.
//   - A fleet with a row is managed only if its Type says so.
//   - An empty fleet id means "belongs to no specific fleet", which is not the
//     same as "Kiwi runs it", so it is denied.
//   - A fleet id with no matching row proves nothing, so it is denied.
//
// A BYOC daemon runs on customer hardware. Sealing a Kiwi key to it would hand
// Kiwi's credential to the customer's machine, and the token counts metered
// against that key would come from a report the same machine produces.
func (s *PostgresStore) IsManagedFleet(ctx context.Context, fleetID string) (bool, error) {
	if fleetID == "" {
		return false, nil
	}
	if fleetID == SharedFreeFleet {
		return true, nil
	}
	var fleets []Fleet
	if err := s.db.WithContext(ctx).
		Where("id = ?", fleetID).
		Limit(1).
		Find(&fleets).Error; err != nil {
		// A lookup failure is not proof of managed operation. Deny.
		return false, err
	}
	if len(fleets) == 0 {
		return false, nil
	}
	return fleets[0].Type == FleetManaged, nil
}
