package orchestrator

import "gorm.io/gorm"

// findByIdempotencyKey returns an existing task for the given key, if any.
// An empty key never matches (idempotency is opt-in).
func findByIdempotencyKey(db *gorm.DB, key string) (*TaskState, bool) {
	if key == "" {
		return nil, false
	}
	var t TaskState
	if err := db.Where("idempotency_key = ?", key).First(&t).Error; err != nil {
		return nil, false
	}
	return &t, true
}
