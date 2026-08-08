package catalog

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/ibreakthecloud/kiwi/pkg/provider"
	"github.com/ibreakthecloud/kiwi/pkg/store"
)

// Store is the slice of persistence the refresher needs. Narrowing it keeps
// the refresh logic testable without a database.
type Store interface {
	ListCredentials(ctx context.Context, orgID string) ([]store.Credential, error)
	GetCredentialPlaintext(ctx context.Context, orgID, name string) (string, error)
	UpsertCatalogModel(ctx context.Context, m *store.CatalogModel) error
	MarkCatalogMissing(ctx context.Context, orgID, providerID string, seen []string, at time.Time) error
}

// Refresher keeps model_catalog current.
type Refresher struct {
	store Store
	// now is injectable so tests can assert on timestamps.
	now func() time.Time
}

func NewRefresher(s Store) *Refresher {
	return &Refresher{store: s}
}

func (r *Refresher) clock() time.Time {
	if r.now != nil {
		return r.now().UTC()
	}
	return time.Now().UTC()
}

// RefreshPlatform re-reads the public model list of every provider Kiwi holds a
// key for, into the global catalog.
//
// A provider with no platform key configured is skipped entirely: there is no
// key to authenticate with, and its models could not be Kiwi-funded anyway.
// That skip is what makes an unset KIWI_PLATFORM_* variable mean "Coming soon"
// rather than "advertised but unusable".
func (r *Refresher) RefreshPlatform(ctx context.Context) {
	var wg sync.WaitGroup
	for _, spec := range provider.Registry() {
		key, ok := provider.PlatformKeyFor(spec.ID)
		if !ok {
			continue
		}
		lister, ok := ListerFor(spec.ID)
		if !ok {
			log.Printf("[catalog] no lister for provider %q; its models cannot be discovered", spec.ID)
			continue
		}
		wg.Add(1)
		go func(sp provider.Spec, lst Lister, apiKey string) {
			defer wg.Done()
			// kiwiKeyAvailable is true here because PlatformKeyFor just said so.
			if err := r.refresh(ctx, store.GlobalCatalogOrg, sp, lst, apiKey, true); err != nil {
				// Logged loudly: a stale catalog is otherwise completely silent,
				// and this line is the only signal that discovery stopped working.
				log.Printf("[catalog] platform refresh for %s failed, keeping existing rows: %v", sp.ID, err)
			}
		}(spec, lister, key)
	}
	wg.Wait()
}

// RefreshOrg re-reads the model lists reachable with an org's own keys.
//
// Providers are resolved from the credential NAME, not its Kind. Kind is "llm"
// or "git" — a category, never a provider id — so looking a provider up by Kind
// matched nothing and every org refresh discovered zero models in silence.
func (r *Refresher) RefreshOrg(ctx context.Context, orgID string) {
	creds, err := r.store.ListCredentials(ctx, orgID)
	if err != nil {
		log.Printf("[catalog] listing credentials for org %s: %v", orgID, err)
		return
	}

	var wg sync.WaitGroup
	for _, cred := range creds {
		spec, ok := specForCredentialName(cred.Name)
		if !ok {
			continue
		}
		lister, ok := ListerFor(spec.ID)
		if !ok {
			continue
		}
		wg.Add(1)
		go func(sp provider.Spec, lst Lister, credName string) {
			defer wg.Done()
			key, err := r.store.GetCredentialPlaintext(ctx, orgID, credName)
			if err != nil || key == "" {
				log.Printf("[catalog] org %s: key %s unavailable, skipping discovery", orgID, credName)
				return
			}
			// Never Kiwi-funded: this list was produced with the customer's own
			// key and describes what THEY can reach, which says nothing about
			// what Kiwi would pay for.
			if err := r.refresh(ctx, orgID, sp, lst, key, false); err != nil {
				log.Printf("[catalog] org %s refresh for %s failed, keeping existing rows: %v", orgID, sp.ID, err)
			}
		}(spec, lister, cred.Name)
	}
	wg.Wait()
}

// RefreshOrgProvider re-reads one provider for one org. Used by the on-save
// trigger, where the provider that changed is known and refreshing the rest
// would be wasted calls against unrelated APIs.
func (r *Refresher) RefreshOrgProvider(ctx context.Context, orgID, credName, key string) error {
	spec, ok := specForCredentialName(credName)
	if !ok {
		return nil
	}
	lister, ok := ListerFor(spec.ID)
	if !ok {
		return nil
	}
	return r.refresh(ctx, orgID, spec, lister, key, false)
}

// refresh lists one provider and reconciles the catalog against what came back.
func (r *Refresher) refresh(ctx context.Context, orgID string, spec provider.Spec, lister Lister, apiKey string, kiwiKeyAvailable bool) error {
	discovered, err := lister.List(ctx, EndpointFor(spec), apiKey)
	if err != nil {
		// Change nothing. A transport failure, a 503, or an unparseable body is
		// not evidence that a provider stopped serving its models — and treating
		// it as such would mark every model missing and empty every user's
		// picker on one bad response. Degrading to a stale catalog is correct;
		// degrading to an empty one is not.
		return err
	}

	at := r.clock()
	seen := make([]string, 0, len(discovered))
	var writeErr error
	for i := range discovered {
		d := discovered[i]
		// Native list endpoints report ids and little else, so price and
		// capability are joined from the static tables here. OpenRouter already
		// supplied both and its values are left alone.
		EnrichFromPricingMap(spec.ID, &d)

		m := &store.CatalogModel{
			OrgID:          orgID,
			ModelID:        d.ID,
			Provider:       spec.ID,
			DisplayName:    d.DisplayName,
			Description:    d.Description,
			InputCostPerM:  d.InputCostPerM,
			OutputCostPerM: d.OutputCostPerM,
			ContextLength:  d.ContextLength,
			SupportsTools:  d.SupportsTools,
			Modality:       d.Modality,
			Source:         "discovered",
			FirstSeenAt:    at,
			LastSeenAt:     at,
		}
		m.ApplyDerived(kiwiKeyAvailable)

		if err := r.store.UpsertCatalogModel(ctx, m); err != nil {
			// One bad row must not abandon the rest of the list — the other
			// models are still worth writing — but it does mean `seen` is now
			// an incomplete record of what the provider serves.
			log.Printf("[catalog] upsert %s/%s: %v", orgID, m.ModelID, err)
			writeErr = err
			continue
		}
		seen = append(seen, d.ID)
	}

	// Reconcile ONLY if every model we saw was written.
	//
	// MarkCatalogMissing treats anything outside `seen` as gone, so a `seen`
	// shortened by write failures marks live models missing — and when every
	// write fails it is empty, which retires the provider's entire catalogue.
	// That is not hypothetical: a migration adding a column was deployed late,
	// every upsert failed on the missing column, and one refresh flipped 400
	// models to selectable=false. The picker emptied and the Models page showed
	// nothing, from a schema drift that should have been inert.
	//
	// The lister failing is already handled above. This is the other half:
	// absence is only evidence when the writes that establish presence worked.
	if writeErr != nil {
		return fmt.Errorf("refresh %s for org %q: %d of %d models written, skipping reconcile: %w",
			spec.ID, orgID, len(seen), len(discovered), writeErr)
	}
	return r.store.MarkCatalogMissing(ctx, orgID, spec.ID, seen, at)
}

// specForCredentialName maps a stored credential name back to its provider.
func specForCredentialName(credName string) (provider.Spec, bool) {
	if credName == "" {
		return provider.Spec{}, false
	}
	for _, spec := range provider.Registry() {
		if spec.CredName == credName {
			return spec, true
		}
	}
	return provider.Spec{}, false
}
