package orchestrator

import (
	"context"
	"log"

	"github.com/ibreakthecloud/kiwi/pkg/auth"
	"github.com/ibreakthecloud/kiwi/pkg/entitlement"
	"github.com/ibreakthecloud/kiwi/pkg/provider"
	"github.com/ibreakthecloud/kiwi/pkg/store"
)

// platformCredsFor returns the Kiwi-owned credentials a leased task may use,
// or an empty map.
//
// Every condition must hold, and every failure denies:
//
//  1. The model resolves, through the catalog, to one Kiwi funds. An inferred
//     resolution never qualifies — inference yields no price.
//  2. Kiwi actually holds a key for that provider.
//  3. The daemon runs on a fleet Kiwi operates. A BYOC daemon runs on customer
//     hardware; sealing a Kiwi key to it hands the credential to the customer.
//  4. The org has allowance left for the model's tier.
//
// The result is scoped to the one provider the leased task needs, rather than
// every platform key Kiwi holds.
func (s *Server) platformCredsFor(ctx context.Context, d *store.Daemon, model string) map[string]string {
	none := map[string]string{}
	if d == nil || model == "" {
		return none
	}

	res, err := s.storage.ResolveModel(ctx, d.OrgID, model)
	if err != nil {
		log.Printf("[platform-key] resolving model %q for org %s: %v", model, d.OrgID, err)
		return none
	}
	if !res.KiwiProvided {
		return none
	}

	key, ok := provider.PlatformKeyFor(res.Provider)
	if !ok {
		return none
	}

	managed, err := s.storage.IsKiwiOperatedFleet(ctx, d.OrgID, d.FleetID)
	if err != nil || !managed {
		if err != nil {
			log.Printf("[platform-key] fleet check for daemon %s: %v", d.ID, err)
		}
		return none
	}

	var org auth.Organization
	if err := s.db.WithContext(ctx).First(&org, "id = ?", d.OrgID).Error; err != nil {
		log.Printf("[platform-key] loading org %s: %v", d.OrgID, err)
		return none
	}

	checker := &entitlement.Checker{Store: s.storage}
	allowed, err := checker.Allow(ctx, d.OrgID, org.Plan, res.Tier)
	if err != nil || !allowed {
		return none
	}

	return map[string]string{provider.CredentialNameFor(res.Provider): key}
}
