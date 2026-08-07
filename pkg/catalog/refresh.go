package catalog

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/ibreakthecloud/kiwi/pkg/provider"
	"github.com/ibreakthecloud/kiwi/pkg/store"
)

// RefresherConfig wraps the dependencies the refresher needs to discover models.
type RefresherConfig struct {
	// PlatformListers are the providers Kiwi funds on behalf of customers
	// (today, just OpenRouter). They populate the global org.
	PlatformListers map[string]Lister
	// NativeListers are providers customers bring their own keys for. They
	// populate the customer's own org.
	NativeListers map[string]Lister
	// PricingLookup resolves prices for native models. If nil, native models
	// will land in TierUnknown and cannot be Kiwi-funded.
	PricingLookup func(modelID string) (*float64, *float64)
}

type Refresher struct {
	store store.Store
	cfg   RefresherConfig
}

func NewRefresher(s store.Store, cfg RefresherConfig) *Refresher {
	return &Refresher{store: s, cfg: cfg}
}

// RefreshPlatform updates the global catalog. It runs asynchronously per
// provider and suppresses errors, so a 503 from one provider does not fail the
// refresh and wipe out the others.
func (r *Refresher) RefreshPlatform(ctx context.Context) error {
	var wg sync.WaitGroup
	for name, lister := range r.cfg.PlatformListers {
		wg.Add(1)
		go func(provName string, provLister Lister) {
			defer wg.Done()
			models, err := provLister.List(ctx, "https://openrouter.ai/api", "")
			if err != nil {
				log.Printf("catalog: platform refresh for %s failed: %v", provName, err)
				return
			}
			r.upsertList(ctx, store.GlobalCatalogOrg, provName, models, true)
		}(name, lister)
	}
	wg.Wait()
	return nil
}

// RefreshOrg updates an org's own catalog rows for every LLM credential it
// holds.
func (r *Refresher) RefreshOrg(ctx context.Context, orgID string) error {
	creds, err := r.store.ListCredentials(ctx, orgID)
	if err != nil {
		return err
	}

	var wg sync.WaitGroup
	for _, cred := range creds {
		spec, ok := provider.SpecFor(cred.Kind)
		if !ok || spec.Kind == "" {
			continue
		}
		lister, ok := r.cfg.NativeListers[cred.Kind]
		if !ok {
			continue
		}

		wg.Add(1)
		go func(c store.Credential, s provider.Spec, lst Lister) {
			defer wg.Done()
			key, err := r.store.GetCredentialPlaintext(ctx, orgID, c.Name)
			if err != nil || key == "" {
				log.Printf("catalog: skipping %s for org %s: key unavailable", c.Kind, orgID)
				return
			}
			models, err := lst.List(ctx, s.BaseURL, key)
			if err != nil {
				log.Printf("catalog: org %s refresh for %s failed: %v", orgID, c.Kind, err)
				return
			}
			if r.cfg.PricingLookup != nil {
				models = EnrichFromPricingMap(models, r.cfg.PricingLookup)
			}
			r.upsertList(ctx, orgID, c.Kind, models, false)
		}(cred, spec, lister)
	}
	wg.Wait()
	return nil
}

func (r *Refresher) upsertList(ctx context.Context, orgID, providerName string, models []DiscoveredModel, isPlatformKey bool) {
	now := time.Now().UTC()
	for _, raw := range models {
		m := &store.CatalogModel{
			OrgID:          orgID,
			ModelID:        raw.ID,
			Provider:       providerName,
			DisplayName:    raw.DisplayName,
			InputCostPerM:  raw.InputCostPerM,
			OutputCostPerM: raw.OutputCostPerM,
			ContextLength:  raw.ContextLength,
			SupportsTools:  raw.SupportsTools,
			Modality:       raw.Modality,
			Source:         "discovered",
			FirstSeenAt:    now,
			LastSeenAt:     now,
		}
		// A BYOK model (orgID != global) bypasses capability checks because
		// native APIs don't return them, and if a customer brings a key they
		// can try the model.
		if orgID != store.GlobalCatalogOrg {
			m.SupportsTools = ptrB(true)
			m.ContextLength = ptrI(128000)
			m.Modality = "text->text"
		}

		m.ApplyDerived(isPlatformKey)

		// Swallow per-row errors so one bad row doesn't break the list.
		if err := r.store.UpsertCatalogModel(ctx, m); err != nil {
			log.Printf("catalog: upsert %s/%s failed: %v", orgID, m.ModelID, err)
		}
	}
}
