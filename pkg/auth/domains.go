package auth

import (
	"context"
	"encoding/hex"
	"strings"
	"time"

	"gorm.io/gorm"
)

// publicEmailProviders is a maintained list of common free email providers.
var publicEmailProviders = map[string]bool{
	"gmail.com":      true,
	"yahoo.com":      true,
	"hotmail.com":    true,
	"outlook.com":    true,
	"aol.com":        true,
	"icloud.com":     true,
	"mail.com":       true,
	"protonmail.com": true,
	"pm.me":          true,
	"zoho.com":       true,
	"yandex.com":     true,
	"gmx.com":        true,
	"gmx.net":        true,
	"me.com":         true,
	"mac.com":        true,
	"live.com":       true,
	"msn.com":        true,
	// test domains
	"github.local": true,
	"test.local":   true,
}

// isPersonalDomain returns true if the domain belongs to a public email provider.
func isPersonalDomain(domain string) bool {
	return publicEmailProviders[strings.ToLower(domain)]
}

// getDomain returns the domain part of an email address.
func getDomain(email string) string {
	parts := strings.Split(email, "@")
	if len(parts) != 2 {
		return ""
	}
	return strings.ToLower(parts[1])
}

// resolveOrgForUser determines which organization a signing-up user belongs to.
// Returns the Organization, whether it was newly created (isNew), and whether the user needs approval (needsApproval).
func resolveOrgForUser(ctx context.Context, db *gorm.DB, email string) (*Organization, bool, bool) {
	domain := getDomain(email)
	if domain == "" || isPersonalDomain(domain) {
		// Create personal org
		orgID := "org_" + hex.EncodeToString([]byte(email))[:8]
		org := &Organization{
			ID:              orgID,
			Name:            email + "'s Workspace",
			Type:            "personal",
			ActivationState: "inactive",
			Plan:            "free",
			CreatedAt:       time.Now(),
		}
		db.Transaction(func(tx *gorm.DB) error {
			if err := tx.Create(org).Error; err != nil {
				return err
			}
			if err := tx.Create(FreeLimits(org.ID)).Error; err != nil {
				return err
			}
			return CreateDefaultFleet(tx, org.ID)
		})
		return org, true, false
	}

	// Company domain
	var org Organization
	err := db.Where("primary_domain = ?", domain).First(&org).Error
	if err != nil {
		// Create new company org
		orgID := "org_" + hex.EncodeToString([]byte(domain))[:8]
		org = Organization{
			ID:              orgID,
			Name:            domain,
			Type:            "team",
			PrimaryDomain:   domain,
			DomainJoin:      false,
			ActivationState: "inactive",
			Plan:            "free",
			CreatedAt:       time.Now(),
		}
		db.Transaction(func(tx *gorm.DB) error {
			if err := tx.Create(&org).Error; err != nil {
				return err
			}
			if err := tx.Create(FreeLimits(org.ID)).Error; err != nil {
				return err
			}
			return CreateDefaultFleet(tx, org.ID)
		})
		return &org, true, false
	}

	// Existing company org
	// S2 stub: we just route to it. S3 handles join logic.
	needsApproval := !org.DomainJoin
	return &org, false, needsApproval
}
