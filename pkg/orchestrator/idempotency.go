package orchestrator

import "gorm.io/gorm"

// findByIdempotencyKey returns an existing task for the given key within the
// specified org. An empty key never matches (idempotency is opt-in).
// Scoped by orgID to prevent cross-tenant idempotency collisions.
func findByIdempotencyKey(db *gorm.DB, key, orgID string) (*TaskState, bool) {
	if key == "" {
		return nil, false
	}
	var t TaskState
	if err := db.Where("idempotency_key = ? AND org_id = ?", key, orgID).First(&t).Error; err != nil {
		return nil, false
	}
	return &t, true
}
