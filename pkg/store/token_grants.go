package store

import (
	"context"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Unlimited is the tokens_granted value meaning "no ceiling".
//
// It is -1, not 0. Zero means no allowance at all, which is a real state — the
// Free plan grants no frontier tokens. org_limits uses 0 for unlimited and has
// already caused a bug that way (see FreeLimits.MaxBudgetPerMonth); repeating
// the convention here would grant unlimited frontier tokens to every org meant
// to have none.
const Unlimited int64 = -1

// OrgTokenGrant is one org's allowance for one tier in one month.
type OrgTokenGrant struct {
	OrgID         string    `gorm:"primaryKey;column:org_id" json:"org_id"`
	Tier          string    `gorm:"primaryKey" json:"tier"`
	Period        string    `gorm:"primaryKey" json:"period"`
	TokensGranted int64     `gorm:"not null;default:0" json:"tokens_granted"`
	TokensUsed    int64     `gorm:"not null;default:0" json:"tokens_used"`
	UpdatedAt     time.Time `gorm:"not null" json:"updated_at"`
}

func (OrgTokenGrant) TableName() string { return "org_token_grants" }

// Remaining reports how many tokens are left. Unlimited grants report -1.
func (g *OrgTokenGrant) Remaining() int64 {
	if g.TokensGranted == Unlimited {
		return Unlimited
	}
	if g.TokensUsed >= g.TokensGranted {
		return 0
	}
	return g.TokensGranted - g.TokensUsed
}

// Exhausted reports whether the allowance is spent. An unlimited grant is never
// exhausted; a zero grant always is.
func (g *OrgTokenGrant) Exhausted() bool {
	if g.TokensGranted == Unlimited {
		return false
	}
	return g.TokensUsed >= g.TokensGranted
}

// CurrentPeriod returns the UTC calendar month a time falls in.
func CurrentPeriod(t time.Time) string {
	return t.UTC().Format("2006-01")
}

// EnsureGrant returns an org's grant for a tier and period, creating it from
// the plan's allowance if it does not exist.
//
// Creation is lazy, on first use, which is why a new month resets the allowance
// without a scheduled job. ON CONFLICT DO NOTHING followed by a read means two
// concurrent tasks cannot double-seed, and an existing row's usage is never
// reset — that would hand out a fresh allowance on every task.
func (s *PostgresStore) EnsureGrant(ctx context.Context, orgID, tier, period string, granted int64) (*OrgTokenGrant, error) {
	row := &OrgTokenGrant{
		OrgID: orgID, Tier: tier, Period: period,
		TokensGranted: granted, TokensUsed: 0, UpdatedAt: time.Now().UTC(),
	}
	if err := s.db.WithContext(ctx).
		Clauses(clause.OnConflict{DoNothing: true}).
		Create(row).Error; err != nil {
		return nil, err
	}

	var out OrgTokenGrant
	if err := s.db.WithContext(ctx).
		Where("org_id = ? AND tier = ? AND period = ?", orgID, tier, period).
		First(&out).Error; err != nil {
		return nil, err
	}
	return &out, nil
}

// ConsumeTokens records usage against a grant.
//
// A single atomic UPDATE rather than a read-modify-write: metering runs
// concurrently across a fleet, and lost updates would undercount usage, which
// is the direction that costs money.
func (s *PostgresStore) ConsumeTokens(ctx context.Context, orgID, tier, period string, n int64) error {
	if n <= 0 {
		return nil
	}
	return s.db.WithContext(ctx).Model(&OrgTokenGrant{}).
		Where("org_id = ? AND tier = ? AND period = ?", orgID, tier, period).
		Updates(map[string]interface{}{
			"tokens_used": gorm.Expr("tokens_used + ?", n),
			"updated_at":  time.Now().UTC(),
		}).Error
}

// ListGrants returns every tier's grant for an org in a period.
func (s *PostgresStore) ListGrants(ctx context.Context, orgID, period string) ([]OrgTokenGrant, error) {
	var out []OrgTokenGrant
	err := s.db.WithContext(ctx).
		Where("org_id = ? AND period = ?", orgID, period).
		Order("tier ASC").
		Find(&out).Error
	return out, err
}

// GetOrgPlan reads an organization's plan type.
func (s *PostgresStore) GetOrgPlan(ctx context.Context, orgID string) (string, error) {
	var plan string
	err := s.db.WithContext(ctx).
		Table("organizations").
		Where("id = ?", orgID).
		Select("plan").
		Scan(&plan).Error
	return plan, err
}
