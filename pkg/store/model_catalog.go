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
