package store

import (
	"context"
	"errors"
	"time"

	"github.com/ibreakthecloud/kiwi/pkg/provider"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// GlobalCatalogOrg is the org_id under which a provider's public model list is
// stored. Empty string rather than NULL keeps the primary key simple and makes
// a duplicate a real conflict.
const GlobalCatalogOrg = ""

const (
	FundingBYOK = "byok"
	FundingKiwi = "kiwi"
)

// CatalogModel is one model Kiwi knows about. It is the authority for routing:
// which provider serves a model, what it costs, and whether it is usable at all.
//
// Cost and capability fields are pointers because unknown is a distinct state
// from zero. A model with a NULL price is not free — it is unpriceable, which
// is why it is never funded by a Kiwi key.
type CatalogModel struct {
	OrgID       string `gorm:"primaryKey;column:org_id" json:"org_id"`
	ModelID     string `gorm:"primaryKey;column:model_id" json:"model_id"`
	Provider    string `gorm:"not null" json:"provider"`
	DisplayName string `gorm:"not null;default:''" json:"display_name"`
	// Description is the provider's own summary of what the model is for,
	// truncated at store time. Empty for providers that do not supply one.
	Description    string     `gorm:"not null;default:''" json:"description"`
	InputCostPerM  *float64   `json:"input_cost_per_m"`
	OutputCostPerM *float64   `json:"output_cost_per_m"`
	ContextLength  *int       `json:"context_length"`
	SupportsTools  *bool      `json:"supports_tools"`
	Modality       string     `gorm:"not null;default:''" json:"modality"`
	Tier           string     `gorm:"not null;default:'unknown'" json:"tier"`
	KiwiProvided   bool       `gorm:"not null;default:false" json:"kiwi_provided"`
	Selectable     bool       `gorm:"not null;default:false" json:"selectable"`
	Source         string     `gorm:"not null;default:'discovered'" json:"source"`
	FirstSeenAt    time.Time  `gorm:"not null" json:"first_seen_at"`
	LastSeenAt     time.Time  `gorm:"not null" json:"last_seen_at"`
	MissingSince   *time.Time `json:"missing_since"`
}

func (CatalogModel) TableName() string { return "model_catalog" }

// UpsertCatalogModel writes a discovered model, updating in place if the row
// already exists. FirstSeenAt is never overwritten: it records when Kiwi first
// saw the model, and a refresh must not reset it.
func (s *PostgresStore) UpsertCatalogModel(ctx context.Context, m *CatalogModel) error {
	return s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "org_id"}, {Name: "model_id"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"provider", "display_name", "description", "input_cost_per_m", "output_cost_per_m",
			"context_length", "supports_tools", "modality", "tier",
			"kiwi_provided", "selectable", "source", "last_seen_at", "missing_since",
		}),
	}).Create(m).Error
}

// ListCatalogModels returns the global catalog plus anything discovered against
// this org's own keys. An org never sees another org's rows.
func (s *PostgresStore) ListCatalogModels(ctx context.Context, orgID string) ([]CatalogModel, error) {
	var out []CatalogModel
	err := s.db.WithContext(ctx).
		Where("org_id IN ?", []string{GlobalCatalogOrg, orgID}).
		Order("provider ASC, model_id ASC").
		Find(&out).Error
	return out, err
}

// GetCatalogModel fetches one row by exact key.
func (s *PostgresStore) GetCatalogModel(ctx context.Context, orgID, modelID string) (*CatalogModel, error) {
	var m CatalogModel
	err := s.db.WithContext(ctx).
		Where("org_id = ? AND model_id = ?", orgID, modelID).
		First(&m).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &m, nil
}

// Grantable tiers. There are three, plus TierUnknown, which is not grantable:
// it means the model could not be priced, and Kiwi does not spend its own money
// on a model whose cost it cannot compute.
const (
	TierFree     = "free"
	TierEconomy  = "economy"
	TierFrontier = "frontier"
	TierUnknown  = "unknown"
)

// economyOutputCeilingPerM is the price band splitting economy from frontier,
// in USD per million output tokens. Output is priced several times higher than
// input across every provider, so it is the axis that decides the band.
const economyOutputCeilingPerM = 2.00

// minContextLength is the smallest window in which the loop actually works.
// The Actor returns whole file contents as JSON and CompletionBudget() defaults
// to 16000 output tokens, so anything smaller cannot round-trip a single-file
// edit. A model below it does not degrade gracefully, it fails mid-task.
const minContextLength = 32000

// DeriveTier places a model in a price band. Unknown pricing yields
// TierUnknown, which is distinct from free: a NULL price is not a zero price.
func DeriveTier(inputPerM, outputPerM *float64) string {
	if inputPerM == nil || outputPerM == nil {
		return TierUnknown
	}
	if *inputPerM == 0 && *outputPerM == 0 {
		return TierFree
	}
	if *outputPerM <= economyOutputCeilingPerM {
		return TierEconomy
	}
	return TierFrontier
}

// DeriveSelectable reports whether a model can be offered in the task form.
// Every condition must hold; each one on its own is enough to break a run.
func DeriveSelectable(m *CatalogModel) bool {
	return m.SupportsTools != nil && *m.SupportsTools &&
		m.ContextLength != nil && *m.ContextLength >= minContextLength &&
		m.Modality == "text->text" &&
		m.MissingSince == nil
}

// ApplyDerived recomputes Tier, Selectable and KiwiProvided from the row's
// facts. kiwiKeyAvailable reports whether Kiwi holds a key for this model's
// provider; it is passed in rather than read from the environment so the
// derivation stays a pure function the tests can drive directly.
func (m *CatalogModel) ApplyDerived(kiwiKeyAvailable bool) {
	m.Tier = DeriveTier(m.InputCostPerM, m.OutputCostPerM)
	m.Selectable = DeriveSelectable(m)
	m.KiwiProvided = kiwiKeyAvailable && m.Tier != TierUnknown
}

// How a Resolution was reached. Inference is a guess and is treated as one:
// it can route a request, but it can never authorise Kiwi to spend money.
const (
	SourceCatalog  = "catalog"
	SourceInferred = "inferred"
)

// Resolution is everything routing needs to know about a model id.
type Resolution struct {
	Provider     string
	Tier         string
	KiwiProvided bool
	Source       string
}

// ResolveModel maps a model id to its provider for an org.
//
// The catalog is authoritative; provider.ProviderOf is the fallback for a miss,
// which is what keeps existing submits and stored org_models rows working. An
// inferred resolution is never Kiwi-funded: inference yields no price, and Kiwi
// does not pay for a model it cannot cost.
func (s *PostgresStore) ResolveModel(ctx context.Context, orgID, modelID string) (Resolution, error) {
	var rows []CatalogModel
	err := s.db.WithContext(ctx).
		Where("model_id = ? AND org_id IN ?", modelID, []string{GlobalCatalogOrg, orgID}).
		Order("org_id DESC").
		Limit(1).
		Find(&rows).Error
	if err != nil {
		return Resolution{}, err
	}
	if len(rows) > 0 {
		m := rows[0]
		return Resolution{
			Provider:     m.Provider,
			Tier:         m.Tier,
			KiwiProvided: m.KiwiProvided,
			Source:       SourceCatalog,
		}, nil
	}
	return Resolution{
		Provider: provider.ProviderOf(modelID),
		Tier:     TierUnknown,
		Source:   SourceInferred,
	}, nil
}

// CheapestKiwiFundedModel returns the lowest-output-cost selectable model
// Kiwi funds for a provider and tier, or ok=false when none qualifies —
// picked at call time against the live catalog rather than a hardcoded id,
// so a caller that needs "some model Kiwi will pay for" always gets the
// current cheapest option instead of one fixed at compile time. orgID scopes
// to that org's own discovered rows in addition to the global catalog; pass
// GlobalCatalogOrg for a lookup with no particular org in play.
//
// output_cost_per_m IS NOT NULL is explicit rather than assumed from tier:
// Tier is a stored column, not recomputed at query time, so a row written
// with TierEconomy and a nil cost (a caller building a CatalogModel by hand
// rather than through ApplyDerived) would otherwise sort first — NULL
// collates before every value in both Postgres ASC and SQLite.
//
// catalogMaturityWindow excludes anything first seen more recently than this:
// picking on price alone handed out qwen/qwen3-coder (failed 4-5 days after
// FirstSeenAt) and inclusionai/ling-2.6-flash (failed 14 days after) in
// production — each newly cheapest at the moment it was picked, each
// unreliable (context-deadline and 429 failures that outlasted every retry).
//
// 21 days rather than a tighter number: both incidents' FirstSeenAt values
// were identical to the microsecond, which means they don't record organic
// per-model discovery — they record a bulk catalog reseed on 2026-08-08, and
// every row touched by that reseed reads as "just discovered" regardless of
// how long the model actually existed before it. FirstSeenAt is contaminated
// by that history and will be again after the next reseed; 21 days is margin
// against the 14-day gap already observed, not a principled bound. The
// catalog has no popularity or uptime signal — FirstSeenAt remains the only
// real one available, so "not recently (re)seen" stands in for "not thinly
// provisioned" until a better signal exists.
const catalogMaturityWindow = 21 * 24 * time.Hour

func (s *PostgresStore) CheapestKiwiFundedModel(ctx context.Context, orgID, providerID, tier string) (string, bool, error) {
	base := s.db.WithContext(ctx).
		Where("org_id IN ? AND provider = ? AND tier = ? AND kiwi_provided = ? AND selectable = ? AND output_cost_per_m IS NOT NULL",
			[]string{GlobalCatalogOrg, orgID}, providerID, tier, true, true).
		Order("output_cost_per_m ASC, model_id ASC")

	var m CatalogModel
	err := base.Session(&gorm.Session{}).
		Where("first_seen_at <= ?", time.Now().Add(-catalogMaturityWindow)).
		Limit(1).First(&m).Error
	if err == nil {
		return m.ModelID, true, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return "", false, err
	}

	// No candidate cleared the maturity bar — most likely every row was
	// (re)seeded together, per catalogMaturityWindow's doc comment, not that
	// every model is genuinely brand new. Falling through to ok=false here
	// would silently starve every Kiwi-funded pick for catalogMaturityWindow
	// after every reseed; defaultWorkerModelFor's only fallback needs a key
	// the org whose default this is doesn't have. Cost-only ranking is the
	// pre-maturity-filter behavior, and it is still strictly better than
	// handing the caller nothing.
	err = base.Session(&gorm.Session{}).Limit(1).First(&m).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", false, nil
		}
		return "", false, err
	}
	return m.ModelID, true, nil
}

// MarkCatalogMissing records that a provider's refresh no longer lists a model,
// and clears the mark on any model that reappeared.
//
// Scoped to one provider and one org: a refresh of OpenRouter must never touch
// an OpenAI row, and one org's refresh must never touch another's. Rows are
// marked rather than deleted because spend rows and execution records join to
// them — deleting turns a past job's model into a blank in the UI.
//
// Callers must only reach here after a SUCCESSFUL list. Calling it with a
// partial or empty list produced by a failed request would mark every model
// missing and empty every picker.
func (s *PostgresStore) MarkCatalogMissing(ctx context.Context, orgID, providerID string, seen []string, at time.Time) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		gone := tx.Model(&CatalogModel{}).
			Where("org_id = ? AND provider = ? AND missing_since IS NULL", orgID, providerID)
		if len(seen) > 0 {
			gone = gone.Where("model_id NOT IN ?", seen)
		}
		if err := gone.Updates(map[string]interface{}{
			"missing_since": at,
			"selectable":    false,
		}).Error; err != nil {
			return err
		}

		if len(seen) == 0 {
			return nil
		}
		// A model that came back must recover, or one transient omission on the
		// provider's side hides it forever.
		return tx.Model(&CatalogModel{}).
			Where("org_id = ? AND provider = ? AND model_id IN ?", orgID, providerID, seen).
			Update("missing_since", nil).Error
	})
}
