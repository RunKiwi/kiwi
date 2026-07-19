package auth

import (
	"crypto/rand"
	"encoding/hex"
	"time"

	"gorm.io/gorm"
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
		if err := tx.Create(&req).Error; err != nil {
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
