package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ErrInstallationNotFound means this org has not installed the GitHub App on
// the account that owns the repository in question. Callers fall back to a
// stored GIT_TOKEN rather than treating it as fatal, because PAT orgs and
// non-GitHub remotes are still supported.
var ErrInstallationNotFound = errors.New("github installation not found")

// GitHubInstallation links one Kiwi org to one GitHub App installation.
//
// The account, not the repository, is the unit: GitHub issues one installation
// per account (user or organisation) and the customer chooses which of that
// account's repositories it covers. So the lookup that matters is "which
// installation covers github.com/<owner>/...", keyed on owner.
type GitHubInstallation struct {
	// InstallationID is GitHub's own id and the primary key. It is what the
	// mint endpoint addresses, and GitHub guarantees its uniqueness.
	InstallationID int64 `gorm:"primaryKey" json:"installation_id"`

	// OrgID scopes every lookup. Without it a caller who learned another
	// tenant's installation id could mint a token against their repositories,
	// so no query in this file omits it except the uninstall path, which is
	// driven by GitHub rather than by a user.
	OrgID string `gorm:"index;not null" json:"org_id"`

	// AccountLogin is the GitHub user or org the App is installed on, stored
	// lower-cased because GitHub treats logins case-insensitively while a
	// database index does not.
	AccountLogin string `gorm:"index;not null" json:"account_login"`

	// RepoSelection is GitHub's "all" or "selected". Recorded for display only:
	// the authoritative answer to "may this token touch that repo" is GitHub's,
	// enforced at mint time, and duplicating it here would go stale.
	RepoSelection string `gorm:"not null;default:selected" json:"repo_selection"`

	CreatedAt time.Time `gorm:"not null;default:current_timestamp" json:"created_at"`
	UpdatedAt time.Time `gorm:"not null;default:current_timestamp" json:"updated_at"`
}

// TableName pins the table name so a future rename of the Go type cannot
// silently orphan the migration.
func (GitHubInstallation) TableName() string { return "github_installations" }

// NormalizeAccountLogin lower-cases a GitHub login for storage and lookup.
func NormalizeAccountLogin(login string) string {
	return strings.ToLower(strings.TrimSpace(login))
}

// UpsertGitHubInstallation records an installation, or re-points an existing
// one at a new org.
//
// Re-pointing is deliberate. A customer who removes the App and reinstalls it
// gets the same installation id back, and if that arrives while the account is
// linked to a different Kiwi org, the most recent explicit install is the one
// the human intended.
func (s *PostgresStore) UpsertGitHubInstallation(ctx context.Context, inst *GitHubInstallation) error {
	if inst == nil {
		return errors.New("installation is required")
	}
	if inst.InstallationID == 0 {
		return errors.New("installation id is required")
	}
	if inst.OrgID == "" {
		return errors.New("org id is required")
	}
	inst.AccountLogin = NormalizeAccountLogin(inst.AccountLogin)
	if inst.AccountLogin == "" {
		return errors.New("account login is required")
	}
	if inst.RepoSelection == "" {
		inst.RepoSelection = "selected"
	}
	inst.UpdatedAt = time.Now().UTC()

	// Two conflicts are possible and only one is handled by an upsert.
	//
	// The same installation re-announcing itself conflicts on installation_id,
	// which OnConflict resolves. But GitHub issues a *new* installation id when
	// an App is removed and installed again, so a reinstall arrives as a new id
	// carrying an account_login that is still spoken for by the old row. That
	// collides with the unique index on account_login, and an upsert keyed on
	// installation_id will not clear it: the reinstall would fail with a
	// constraint error naming an index the operator has never heard of.
	//
	// Clearing stale rows for the account first makes the newest explicit
	// install win, which is what the human doing the reinstalling intended.
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("account_login = ? AND installation_id <> ?", inst.AccountLogin, inst.InstallationID).
			Delete(&GitHubInstallation{}).Error; err != nil {
			return fmt.Errorf("clear stale installation for %s: %w", inst.AccountLogin, err)
		}
		return tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "installation_id"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"org_id", "account_login", "repo_selection", "updated_at",
			}),
		}).Create(inst).Error
	})
}

// FindGitHubInstallation returns the installation covering accountLogin for
// this org, or ErrInstallationNotFound.
//
// orgID is not optional and is not a filter applied after the fact: it is the
// boundary that stops one tenant minting tokens against another's repositories.
func (s *PostgresStore) FindGitHubInstallation(ctx context.Context, orgID, accountLogin string) (*GitHubInstallation, error) {
	if orgID == "" {
		return nil, errors.New("org id is required")
	}
	login := NormalizeAccountLogin(accountLogin)
	if login == "" {
		return nil, ErrInstallationNotFound
	}

	var inst GitHubInstallation
	err := s.db.WithContext(ctx).
		Where("org_id = ? AND account_login = ?", orgID, login).
		First(&inst).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrInstallationNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("find github installation: %w", err)
	}
	return &inst, nil
}

// ListGitHubInstallations returns every installation linked to an org.
func (s *PostgresStore) ListGitHubInstallations(ctx context.Context, orgID string) ([]GitHubInstallation, error) {
	if orgID == "" {
		return nil, errors.New("org id is required")
	}
	var out []GitHubInstallation
	if err := s.db.WithContext(ctx).
		Where("org_id = ?", orgID).
		Order("account_login ASC").
		Find(&out).Error; err != nil {
		return nil, fmt.Errorf("list github installations: %w", err)
	}
	return out, nil
}

// GetGitHubInstallationByID resolves the org that installed the GitHub App
// with installationID — the identity every GitHub App webhook delivery
// carries in its top-level "installation" field, and the only tenant-scoping
// signal available on events (check_run, a revert PR opened by someone other
// than Kiwi) that carry no QueuedTask to resolve org through.
//
// Deliberately not org-scoped in its own arguments, same reasoning as
// DeleteGitHubInstallation: the installation id is the thing GitHub asserts,
// and resolving org FROM it is exactly this function's job.
func (s *PostgresStore) GetGitHubInstallationByID(ctx context.Context, installationID int64) (*GitHubInstallation, error) {
	var inst GitHubInstallation
	err := s.db.WithContext(ctx).
		Where("installation_id = ?", installationID).
		First(&inst).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrInstallationNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get github installation by id: %w", err)
	}
	return &inst, nil
}

// DeleteGitHubInstallation removes a link, for the `installation.deleted`
// webhook.
//
// Deliberately not scoped by org: GitHub is the authority on whether an
// installation still exists, and refusing to delete because our org mapping
// drifted would leave a row that mints nothing but still routes tasks away from
// the PAT fallback.
func (s *PostgresStore) DeleteGitHubInstallation(ctx context.Context, installationID int64) error {
	return s.db.WithContext(ctx).
		Where("installation_id = ?", installationID).
		Delete(&GitHubInstallation{}).Error
}
