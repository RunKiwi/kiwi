package store

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"time"
)

// Fleet is a named group of execution capacity for an org. Type is either
// self-managed (Kiwi operates the daemons) or byoc (the customer runs them).
const (
	FleetSelfManaged = "self-managed"
	FleetBYOC        = "byoc"
)

type Fleet struct {
	ID        string    `gorm:"primaryKey" json:"id"`
	OrgID     string    `gorm:"index;not null" json:"org_id"`
	Name      string    `gorm:"not null" json:"name"`
	Type      string    `gorm:"not null" json:"type"` // self-managed | byoc
	CreatedAt time.Time `gorm:"not null;default:current_timestamp" json:"created_at"`
}

func (Fleet) TableName() string { return "fleets" }

// ModelEntry is an LLM model the org has made available in the UI (in addition
// to the built-in defaults). Name is the API model id (e.g. gemini-2.0-flash);
// the daemon routes gemini-* to Gemini, else Anthropic.
type ModelEntry struct {
	ID        string    `gorm:"primaryKey" json:"id"`
	OrgID     string    `gorm:"index;not null" json:"org_id"`
	Name      string    `gorm:"not null" json:"name"`
	Provider  string    `json:"provider"` // anthropic | gemini | codex
	CreatedAt time.Time `gorm:"not null;default:current_timestamp" json:"created_at"`
}

func (ModelEntry) TableName() string { return "org_models" }

func NewDashID(prefix string) string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return prefix + "_" + hex.EncodeToString(b)
}

// --- Fleets ---

func (s *PostgresStore) CreateFleet(ctx context.Context, orgID, name, ftype string) (*Fleet, error) {
	if ftype != FleetSelfManaged && ftype != FleetBYOC {
		ftype = FleetSelfManaged
	}
	f := &Fleet{ID: NewDashID("flt"), OrgID: orgID, Name: name, Type: ftype, CreatedAt: time.Now()}
	if err := s.db.WithContext(ctx).Create(f).Error; err != nil {
		return nil, err
	}
	return f, nil
}

func (s *PostgresStore) ListFleets(ctx context.Context, orgID string) ([]Fleet, error) {
	var fleets []Fleet
	if err := s.db.WithContext(ctx).Where("org_id = ?", orgID).Order("created_at DESC").Find(&fleets).Error; err != nil {
		return nil, err
	}
	return fleets, nil
}

// --- Models ---

func (s *PostgresStore) CreateModel(ctx context.Context, orgID, name, provider string) (*ModelEntry, error) {
	m := &ModelEntry{ID: NewDashID("mdl"), OrgID: orgID, Name: name, Provider: provider, CreatedAt: time.Now()}
	if err := s.db.WithContext(ctx).Create(m).Error; err != nil {
		return nil, err
	}
	return m, nil
}

func (s *PostgresStore) ListModels(ctx context.Context, orgID string) ([]ModelEntry, error) {
	var models []ModelEntry
	if err := s.db.WithContext(ctx).Where("org_id = ?", orgID).Order("created_at DESC").Find(&models).Error; err != nil {
		return nil, err
	}
	return models, nil
}

func (s *PostgresStore) DeleteModel(ctx context.Context, orgID, id string) error {
	// Org-scoped delete: a caller cannot delete another org's model.
	return s.db.WithContext(ctx).Where("org_id = ? AND id = ?", orgID, id).Delete(&ModelEntry{}).Error
}
