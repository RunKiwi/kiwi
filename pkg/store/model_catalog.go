package store

import (
	"context"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// GlobalCatalogOrg is the org_id under which a provider's public model list is
// stored. Empty string rather than NULL keeps the primary key simple and makes
// a duplicate a real conflict.
const GlobalCatalogOrg = ""

// CatalogModel is one model Kiwi knows about. It is the authority for routing:
// which provider serves a model, what it costs, and whether it is usable at all.
//
// Cost and capability fields are pointers because unknown is a distinct state
// from zero. A model with a NULL price is not free — it is unpriceable, which
// is why it is never funded by a Kiwi key.
type CatalogModel struct {
	OrgID          string     `gorm:"primaryKey;column:org_id" json:"org_id"`
	ModelID        string     `gorm:"primaryKey;column:model_id" json:"model_id"`
	Provider       string     `gorm:"not null" json:"provider"`
	DisplayName    string     `gorm:"not null;default:''" json:"display_name"`
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
			"provider", "display_name", "input_cost_per_m", "output_cost_per_m",
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
