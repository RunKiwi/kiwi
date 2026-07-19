package auth

import (
	"context"
	"testing"
)

func TestIsPersonalDomain(t *testing.T) {
	tests := []struct {
		domain   string
		expected bool
	}{
		{"gmail.com", true},
		{"GMAIL.COM", true},
		{"yahoo.com", true},
		{"acmecorp.com", false},
		{"google.com", false},
		{"runkiwi.com", false},
	}

	for _, tc := range tests {
		t.Run(tc.domain, func(t *testing.T) {
			if got := isPersonalDomain(tc.domain); got != tc.expected {
				t.Errorf("isPersonalDomain(%q) = %v; want %v", tc.domain, got, tc.expected)
			}
		})
	}
}

func TestGetDomain(t *testing.T) {
	if got := getDomain("user@AcmeCorp.com"); got != "acmecorp.com" {
		t.Errorf("expected acmecorp.com, got %v", got)
	}
	if got := getDomain("invalid-email"); got != "" {
		t.Errorf("expected empty string, got %v", got)
	}
}

func TestResolveOrgForUser(t *testing.T) {
	db := setupTestDB(t)

	// Test personal domain
	org, isNew, needsApproval := resolveOrgForUser(context.Background(), db, "test@gmail.com")
	if org.Type != "personal" || !isNew || needsApproval {
		t.Errorf("expected new personal org without approval, got %v, %v, %v", org.Type, isNew, needsApproval)
	}

	// Test new company domain
	org1, isNew1, needsApproval1 := resolveOrgForUser(context.Background(), db, "alice@acmecorp.com")
	if org1.Type != "team" || org1.PrimaryDomain != "acmecorp.com" || !isNew1 || needsApproval1 {
		t.Errorf("expected new team org without approval, got %+v, %v, %v", org1, isNew1, needsApproval1)
	}

	// Test existing company domain, DomainJoin off (default)
	org2, isNew2, needsApproval2 := resolveOrgForUser(context.Background(), db, "bob@acmecorp.com")
	if org2.ID != org1.ID || isNew2 || !needsApproval2 {
		t.Errorf("expected existing team org with approval needed, got %+v, %v, %v", org2, isNew2, needsApproval2)
	}

	// Test existing company domain, DomainJoin on
	org1.DomainJoin = true
	db.Save(&org1)

	org3, isNew3, needsApproval3 := resolveOrgForUser(context.Background(), db, "charlie@acmecorp.com")
	if org3.ID != org1.ID || isNew3 || needsApproval3 {
		t.Errorf("expected existing team org without approval needed, got %+v, %v, %v", org3, isNew3, needsApproval3)
	}
}
