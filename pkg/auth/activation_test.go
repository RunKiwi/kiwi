package auth

import (
	"testing"
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
