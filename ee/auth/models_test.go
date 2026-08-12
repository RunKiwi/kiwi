// SPDX-License-Identifier: LicenseRef-Kiwi-BSL-1.1
// Copyright (c) 2026 RunKiwi. Licensed under the Business Source License 1.1.
// See ee/LICENSE. This is Control Plane code and is NOT Apache-2.0.

package auth

import "testing"

func TestOrganization_CanRun(t *testing.T) {
	tests := []struct {
		name     string
		state    string
		expected bool
	}{
		{"active", "active", true},
		{"inactive", "inactive", false},
		{"suspended", "suspended", false},
		{"empty", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			org := &Organization{ActivationState: tt.state}
			if got := org.CanRun(); got != tt.expected {
				t.Errorf("CanRun() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestOrganization_Defaults(t *testing.T) {
	db := setupTestDB(t)

	org := Organization{ID: "org-def", Name: "Default Org"}
	if err := db.Create(&org).Error; err != nil {
		t.Fatalf("failed to create org: %v", err)
	}

	var fetched Organization
	if err := db.First(&fetched, "id = ?", "org-def").Error; err != nil {
		t.Fatalf("failed to fetch org: %v", err)
	}

	if fetched.Type != "personal" {
		t.Errorf("expected type 'personal', got %q", fetched.Type)
	}
	if fetched.Plan != "free" {
		t.Errorf("expected plan 'free', got %q", fetched.Plan)
	}
	// Active on creation. "inactive" described a gate no code enforced, and it
	// disarmed SuspendOrg for the orgs that had never been touched — see
	// signup_activation_test.go.
	if fetched.ActivationState != "active" {
		t.Errorf("expected state 'active', got %q", fetched.ActivationState)
	}
	if fetched.PrimaryDomain != "" {
		t.Errorf("expected primary_domain '', got %q", fetched.PrimaryDomain)
	}
	if fetched.DomainJoin != false {
		t.Errorf("expected domain_join false, got %v", fetched.DomainJoin)
	}
}
