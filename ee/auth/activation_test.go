// SPDX-License-Identifier: LicenseRef-Kiwi-BSL-1.1
// Copyright (c) 2026 RunKiwi. Licensed under the Business Source License 1.1.
// See ee/LICENSE. This is Control Plane code and is NOT Apache-2.0.

package auth

import (
	"testing"
	"time"
)

func TestActivateSuspendOrg(t *testing.T) {
	db := setupTestDB(t)

	org := Organization{
		ID:              "org_test_activation",
		Name:            "Acme",
		Type:            "team",
		ActivationState: "inactive",
	}
	db.Create(&org)

	if err := ActivateOrg(db, "org_test_activation"); err != nil {
		t.Fatalf("Failed to activate org: %v", err)
	}

	var updatedOrg Organization
	db.First(&updatedOrg, "id = ?", "org_test_activation")
	if updatedOrg.ActivationState != "active" {
		t.Errorf("expected state active, got %s", updatedOrg.ActivationState)
	}

	var provReq ProvisioningRequest
	if err := db.Where("org_id = ? AND type = ?", "org_test_activation", "provision").First(&provReq).Error; err != nil {
		t.Errorf("expected provision request to be enqueued: %v", err)
	}
	if provReq.Status != "pending" {
		t.Errorf("expected pending status, got %s", provReq.Status)
	}

	// Test suspend
	if err := SuspendOrg(db, "org_test_activation"); err != nil {
		t.Fatalf("Failed to suspend org: %v", err)
	}

	db.First(&updatedOrg, "id = ?", "org_test_activation")
	if updatedOrg.ActivationState != "suspended" {
		t.Errorf("expected state suspended, got %s", updatedOrg.ActivationState)
	}

	var reclaimReq ProvisioningRequest
	if err := db.Where("org_id = ? AND type = ?", "org_test_activation", "reclaim").First(&reclaimReq).Error; err != nil {
		t.Errorf("expected reclaim request to be enqueued: %v", err)
	}
	if reclaimReq.Status != "pending" {
		t.Errorf("expected pending status, got %s", reclaimReq.Status)
	}
}

// A free-tier submit enqueues a pending provision request via the cold-start
// path. Activating afterwards used to insert a second one, hit the partial
// unique index, and roll the whole transaction back — so the org silently
// stayed inactive and the admin UI showed a duplicate-key error.
func TestActivateOrgWithPendingProvisionRequest(t *testing.T) {
	db := setupTestDB(t)

	// The production index, from provisioner.EnsureSchema. Without it this test
	// passes for the wrong reason: nothing would reject the duplicate insert.
	if err := db.Exec(`CREATE UNIQUE INDEX IF NOT EXISTS idx_prov_one_pending_provision ` +
		`ON provisioning_requests (org_id) WHERE status = 'pending' AND type = 'provision'`).Error; err != nil {
		t.Fatalf("create partial index: %v", err)
	}

	const orgID = "org_pending_prov"
	if err := db.Create(&Organization{ID: orgID, Name: "Acme", ActivationState: "inactive"}).Error; err != nil {
		t.Fatalf("create org: %v", err)
	}
	// Stand in for the cold-start enqueue that a submit would have done.
	if err := db.Create(&ProvisioningRequest{
		ID: "prov_existing", OrgID: orgID, Type: "provision", Status: "pending", CreatedAt: time.Now(),
	}).Error; err != nil {
		t.Fatalf("seed provision request: %v", err)
	}

	if err := ActivateOrg(db, orgID); err != nil {
		t.Fatalf("activation must not fail when a provision request is already pending: %v", err)
	}

	var org Organization
	if err := db.First(&org, "id = ?", orgID).Error; err != nil {
		t.Fatalf("reload org: %v", err)
	}
	if org.ActivationState != "active" {
		t.Errorf("ActivationState = %q, want %q — the transaction rolled back", org.ActivationState, "active")
	}

	// The pre-existing request is enough; a second would be redundant work.
	var n int64
	db.Model(&ProvisioningRequest{}).
		Where("org_id = ? AND type = ? AND status = ?", orgID, "provision", "pending").Count(&n)
	if n != 1 {
		t.Errorf("pending provision requests = %d, want 1", n)
	}
}
