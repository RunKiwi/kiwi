// SPDX-License-Identifier: LicenseRef-Kiwi-BSL-1.1
// Copyright (c) 2026 RunKiwi. Licensed under the Business Source License 1.1.
// See ee/LICENSE. This is Control Plane code and is NOT Apache-2.0.

package auth

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/ibreakthecloud/kiwi/ee/billing"
	"github.com/ibreakthecloud/kiwi/ee/entitlement"
	"github.com/ibreakthecloud/kiwi/pkg/store"
	"gorm.io/gorm"
)

// BillingWebhookHandler processes Stripe webhook events (checkout completed,
// subscription created/deleted/paused) and moves the org's plan accordingly.
// It verifies the Stripe-Signature header against STRIPE_WEBHOOK_SECRET, so an
// unauthenticated caller can never upgrade an org.
func BillingWebhookHandler(db *gorm.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		secret := os.Getenv("STRIPE_WEBHOOK_SECRET")
		if secret == "" {
			http.Error(w, "Billing webhooks disabled", http.StatusServiceUnavailable)
			return
		}

		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "Failed to read body", http.StatusBadRequest)
			return
		}

		if err := billing.VerifyStripeSignature(body, r.Header.Get("Stripe-Signature"), secret); err != nil {
			http.Error(w, "Invalid signature", http.StatusUnauthorized)
			return
		}

		var payload struct {
			Type string `json:"type"`
			Data struct {
				Object struct {
					Metadata struct {
						OrgID string `json:"org_id"`
						Plan  string `json:"plan"`
					} `json:"metadata"`
				} `json:"object"`
			} `json:"data"`
		}

		if err := json.Unmarshal(body, &payload); err != nil {
			http.Error(w, "Invalid payload", http.StatusBadRequest)
			return
		}

		orgID := payload.Data.Object.Metadata.OrgID
		if orgID == "" {
			// Not a kiwi-managed checkout, ignore
			w.WriteHeader(http.StatusOK)
			return
		}

		switch payload.Type {
		case "checkout.session.completed", "customer.subscription.created", "customer.subscription.updated":
			plan := payload.Data.Object.Metadata.Plan
			if plan != "" {
				_ = UpdateOrgPlanAndLimits(db, orgID, plan)
			}
		case "customer.subscription.deleted", "customer.subscription.paused":
			if err := SuspendOrg(db, orgID); err != nil {
				http.Error(w, "Failed to suspend org", http.StatusInternalServerError)
				return
			}
		}

		w.WriteHeader(http.StatusOK)
	}
}

func UpdateOrgPlanAndLimits(db *gorm.DB, orgID, plan string) error {
	return db.Transaction(func(tx *gorm.DB) error {
		var org Organization
		if err := tx.First(&org, "id = ?", orgID).Error; err != nil {
			return err
		}
		org.Plan = plan
		if err := tx.Save(&org).Error; err != nil {
			return err
		}

		updates := map[string]interface{}{}
		switch plan {
		case "team":
			updates["max_concurrent_jobs"] = 50
			updates["max_budget_per_job"] = 20.0
			updates["max_budget_per_month"] = 5000.0
			updates["max_agent_minutes_per_month"] = 5000.0
		case "pro", "individual": // "pro" is the current paid tier; "individual" kept for back-compat
			updates["max_concurrent_jobs"] = 20
			updates["max_budget_per_job"] = 10.0
			updates["max_budget_per_month"] = 1000.0
			updates["max_agent_minutes_per_month"] = 2000.0
		default: // free
			updates["max_concurrent_jobs"] = 1
			updates["max_budget_per_job"] = 2.0
			updates["max_budget_per_month"] = 500.0
			updates["max_agent_minutes_per_month"] = 500.0
		}
		if err := tx.Table("org_limits").Where("org_id = ?", orgID).Updates(updates).Error; err != nil {
			return err
		}

		// Also upgrade org_token_grants for the current period so that higher token allowances apply immediately
		if tx.Migrator().HasTable("org_token_grants") {
			currentPeriod := time.Now().UTC().Format("2006-01")
			for _, g := range entitlement.PlanGrants(plan) {
				if g.Tokens == store.Unlimited {
					_ = tx.Table("org_token_grants").
						Where("org_id = ? AND tier = ? AND period = ?", orgID, g.Tier, currentPeriod).
						Update("tokens_granted", store.Unlimited).Error
				} else {
					_ = tx.Table("org_token_grants").
						Where("org_id = ? AND tier = ? AND period = ? AND tokens_granted != ? AND tokens_granted < ?", orgID, g.Tier, currentPeriod, store.Unlimited, g.Tokens).
						Update("tokens_granted", g.Tokens).Error
				}
			}
		}

		return nil
	})
}
