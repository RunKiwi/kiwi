package auth

import (
	"crypto/rand"
	"encoding/hex"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ActivateOrg activates the organization and enqueues a provisioning request.
func ActivateOrg(db *gorm.DB, orgID string) error {
	return db.Transaction(func(tx *gorm.DB) error {
		var org Organization
		if err := tx.First(&org, "id = ?", orgID).Error; err != nil {
			return err
		}

		if org.ActivationState == "active" {
			return nil // Already active
		}

		org.ActivationState = "active"
		if err := tx.Save(&org).Error; err != nil {
			return err
		}

		reqIDBytes := make([]byte, 8)
		rand.Read(reqIDBytes)
		req := ProvisioningRequest{
			ID:        "prov_" + hex.EncodeToString(reqIDBytes),
			OrgID:     orgID,
			Type:      "provision",
			Status:    "pending",
			CreatedAt: time.Now(),
		}
		// An org can already have a pending provision request — a submit enqueues
		// one via the free-tier cold start. The idx_prov_one_pending_provision
		// partial unique index then rejects this insert, and because the insert
		// shares the transaction with the status change, the activation rolls back
		// with it: the org stays inactive and the admin UI reports a duplicate-key
		// error. A second request would be redundant anyway, so skip it.
		// Matches planner.ensureFreeDaemon, which already inserts this way.
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&req).Error; err != nil {
			return err
		}

		return nil
	})
}

// SuspendOrg suspends the organization and enqueues a reclaim request.
func SuspendOrg(db *gorm.DB, orgID string) error {
	return db.Transaction(func(tx *gorm.DB) error {
		var org Organization
		if err := tx.First(&org, "id = ?", orgID).Error; err != nil {
			return err
		}

		if org.ActivationState == "suspended" || org.ActivationState == "inactive" {
			return nil // Already suspended/inactive
		}

		org.ActivationState = "suspended"
		if err := tx.Save(&org).Error; err != nil {
			return err
		}

		reqIDBytes := make([]byte, 8)
		rand.Read(reqIDBytes)
		req := ProvisioningRequest{
			ID:        "prov_" + hex.EncodeToString(reqIDBytes),
			OrgID:     orgID,
			Type:      "reclaim",
			Status:    "pending",
			CreatedAt: time.Now(),
		}
		if err := tx.Create(&req).Error; err != nil {
			return err
		}

		return nil
	})
}
