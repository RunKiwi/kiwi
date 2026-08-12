// SPDX-License-Identifier: LicenseRef-Kiwi-BSL-1.1
// Copyright (c) 2026 RunKiwi. Licensed under the Business Source License 1.1.
// See ee/LICENSE. This is Control Plane code and is NOT Apache-2.0.

package auth

import (
	"context"
	"testing"
)

// A new org is active from the moment it exists.
//
// It was created "inactive" and an operator flipped it by hand, which read like
// an approval queue and was not one: nothing in the run path has ever consulted
// the flag. The submit handler gates on "suspended" specifically, ensureFreeDaemon
// enqueues provisioning without looking, and the provisioner claims any pending
// request. So the label said "this org cannot run" about orgs that could, and the
// operator clicking Activate changed nothing except the label.
//
// Fixing the label rather than the gate is deliberate. Making activation
// meaningful would lock out every free org, which is the outcome the submit
// handler's comment already records as rejected.
func TestNewPersonalOrgIsActiveOnCreation(t *testing.T) {
	db := setupTestDB(t)

	org, isNew, needsApproval := resolveOrgForUser(context.Background(), db, "someone@gmail.com")
	if !isNew || needsApproval {
		t.Fatalf("expected a fresh personal org, got isNew=%v needsApproval=%v", isNew, needsApproval)
	}
	if org.ActivationState != "active" {
		t.Errorf("ActivationState = %q, want active: a free org that can run must not be labelled as if it cannot", org.ActivationState)
	}
	if !org.CanRun() {
		t.Error("CanRun() is false for an org the submit path will happily accept work from")
	}
}

func TestNewCompanyOrgIsActiveOnCreation(t *testing.T) {
	db := setupTestDB(t)

	org, isNew, _ := resolveOrgForUser(context.Background(), db, "someone@acme-unseen-domain.com")
	if !isNew {
		t.Fatal("expected a fresh company org")
	}
	if org.ActivationState != "active" {
		t.Errorf("ActivationState = %q, want active", org.ActivationState)
	}
}

// Signup must not cold-start a daemon. Provisioning is enqueued on SUBMIT
// (ensureFreeDaemon), which is when a runner is actually wanted; doing it at
// signup would put a container on the shared fleet for every account that never
// runs anything.
func TestSignupDoesNotEnqueueProvisioning(t *testing.T) {
	db := setupTestDB(t)

	org, _, _ := resolveOrgForUser(context.Background(), db, "someone@gmail.com")

	var n int64
	if err := db.Model(&ProvisioningRequest{}).Where("org_id = ?", org.ID).Count(&n).Error; err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Errorf("signup enqueued %d provisioning request(s); a runner is provisioned on submit, not on signup", n)
	}
}

// Suspension is the state that does gate the run path, and it must keep working
// — an org auto-suspended for abuse must not read as active.
func TestSuspensionStillOverridesActive(t *testing.T) {
	db := setupTestDB(t)

	org, _, _ := resolveOrgForUser(context.Background(), db, "abuser@gmail.com")
	if err := SuspendOrg(db, org.ID); err != nil {
		t.Fatalf("SuspendOrg: %v", err)
	}

	var got Organization
	if err := db.First(&got, "id = ?", org.ID).Error; err != nil {
		t.Fatal(err)
	}
	if got.ActivationState != "suspended" {
		t.Errorf("ActivationState = %q, want suspended", got.ActivationState)
	}
	if got.CanRun() {
		t.Error("a suspended org must not report CanRun")
	}
}
