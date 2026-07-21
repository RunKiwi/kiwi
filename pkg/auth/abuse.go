package auth

import (
	"time"

	"gorm.io/gorm"
)

const (
	// DefaultAbuseStrikeThreshold is how many abuse strikes within the rolling
	// window auto-suspend an org. A single strike is a weak signal — the daemon's
	// cryptomining heuristic (task hit its wall-clock timeout with <2 loop steps)
	// also fires for a legitimately slow test suite — so suspending a whole org on
	// one strike is too aggressive. Require repeats before acting.
	DefaultAbuseStrikeThreshold = 3
	// AbuseStrikeWindow is the rolling window over which strikes accumulate.
	// Strikes older than this decay (the counter restarts), so occasional slow
	// tasks spread over time never add up to a suspend.
	AbuseStrikeWindow = time.Hour
)

// RecordAbuseStrike records one abuse strike for an org and auto-suspends it when
// the strike count within the rolling window reaches the threshold. A strike
// arriving after the window elapsed restarts the count at 1 (decay). Returns
// whether the org was suspended and the strike count reached. A non-positive
// threshold or window falls back to the defaults.
func RecordAbuseStrike(db *gorm.DB, orgID string, threshold int, window time.Duration) (suspended bool, strikes int, err error) {
	if threshold <= 0 {
		threshold = DefaultAbuseStrikeThreshold
	}
	if window <= 0 {
		window = AbuseStrikeWindow
	}

	err = db.Transaction(func(tx *gorm.DB) error {
		var org Organization
		if err := tx.First(&org, "id = ?", orgID).Error; err != nil {
			return err
		}

		now := time.Now()
		if org.LastAbuseAt == nil || now.Sub(*org.LastAbuseAt) > window {
			org.AbuseStrikes = 1 // first strike, or the previous one has decayed
		} else {
			org.AbuseStrikes++
		}
		org.LastAbuseAt = &now

		strikes = org.AbuseStrikes
		suspended = org.AbuseStrikes >= threshold
		if suspended {
			// Clear so a reinstated org starts from a clean slate.
			org.AbuseStrikes = 0
		}
		return tx.Save(&org).Error
	})
	if err != nil {
		return false, 0, err
	}

	if suspended {
		// SuspendOrg flips ActivationState to "suspended" (blocking the submit
		// path) and enqueues a daemon reclaim. Its own transaction.
		if err := SuspendOrg(db, orgID); err != nil {
			return true, strikes, err
		}
	}
	return suspended, strikes, nil
}
