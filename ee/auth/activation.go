// SPDX-License-Identifier: LicenseRef-Kiwi-BSL-1.1
// Copyright (c) 2026 RunKiwi. Licensed under the Business Source License 1.1.
// See ee/LICENSE. This is Control Plane code and is NOT Apache-2.0.

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

		// "inactive" is deliberately NOT treated as already-suspended. It used to
		// be, on the assumption that an inactive org could not run — but nothing
		// in the run path ever checked: the submit handler gates on "suspended"
		// specifically, and the provisioner claims any pending request. So an
		// abusive org that no operator had activated could not be auto-suspended,
		// RecordAbuseStrike's call here quietly did nothing, and no daemon
		// reclaim was enqueued. That is the exact population most likely to be
		// abusive — a fresh signup nobody has touched.
		if org.ActivationState == "suspended" {
			return nil // Already suspended
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
