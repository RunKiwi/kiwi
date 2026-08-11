package store

import (
	"context"
	"errors"
	"testing"
)

func TestFindGitHubInstallationRoundTrip(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	if err := s.UpsertGitHubInstallation(ctx, &GitHubInstallation{
		InstallationID: 555,
		OrgID:          "org_a",
		AccountLogin:   "RunKiwi",
		RepoSelection:  "selected",
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	// Stored and looked up case-insensitively: GitHub treats logins that way
	// and a webhook may spell the account differently from the install callback.
	got, err := s.FindGitHubInstallation(ctx, "org_a", "runkiwi")
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if got.InstallationID != 555 {
		t.Errorf("installation id = %d, want 555", got.InstallationID)
	}
	if got.AccountLogin != "runkiwi" {
		t.Errorf("account login = %q, want it folded to lower case", got.AccountLogin)
	}
}

// The tenancy boundary. An org that learns another tenant's account name must
// not be able to resolve it, because resolving it is what mints a token.
func TestFindGitHubInstallationIsOrgScoped(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	if err := s.UpsertGitHubInstallation(ctx, &GitHubInstallation{
		InstallationID: 777,
		OrgID:          "org_victim",
		AccountLogin:   "victim-co",
	}); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	_, err := s.FindGitHubInstallation(ctx, "org_attacker", "victim-co")
	if !errors.Is(err, ErrInstallationNotFound) {
		t.Fatalf("err = %v, want ErrInstallationNotFound: one org resolved another org's installation", err)
	}
}

func TestFindGitHubInstallationMissing(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.FindGitHubInstallation(context.Background(), "org_a", "nobody"); !errors.Is(err, ErrInstallationNotFound) {
		t.Fatalf("err = %v, want ErrInstallationNotFound", err)
	}
}

// A reinstall arrives as a brand new installation id for an account that is
// still spoken for by the old row. It must replace it rather than collide with
// the unique index on account_login.
func TestUpsertGitHubInstallationSurvivesReinstall(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	if err := s.UpsertGitHubInstallation(ctx, &GitHubInstallation{
		InstallationID: 100,
		OrgID:          "org_a",
		AccountLogin:   "acme",
	}); err != nil {
		t.Fatalf("first install: %v", err)
	}
	if err := s.UpsertGitHubInstallation(ctx, &GitHubInstallation{
		InstallationID: 200,
		OrgID:          "org_a",
		AccountLogin:   "acme",
	}); err != nil {
		t.Fatalf("reinstall: %v", err)
	}

	got, err := s.FindGitHubInstallation(ctx, "org_a", "acme")
	if err != nil {
		t.Fatalf("find after reinstall: %v", err)
	}
	if got.InstallationID != 200 {
		t.Errorf("installation id = %d, want 200 (newest install must win)", got.InstallationID)
	}

	all, err := s.ListGitHubInstallations(ctx, "org_a")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(all) != 1 {
		t.Errorf("org has %d installations, want 1 (stale row not cleared)", len(all))
	}
}

// Re-pointing an account at a different org is how a customer moves a repo
// between Kiwi orgs, and the newest explicit install is the intended one.
func TestUpsertGitHubInstallationRepoints(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	_ = s.UpsertGitHubInstallation(ctx, &GitHubInstallation{
		InstallationID: 300, OrgID: "org_old", AccountLogin: "acme",
	})
	if err := s.UpsertGitHubInstallation(ctx, &GitHubInstallation{
		InstallationID: 300, OrgID: "org_new", AccountLogin: "acme",
	}); err != nil {
		t.Fatalf("repoint: %v", err)
	}

	if _, err := s.FindGitHubInstallation(ctx, "org_old", "acme"); !errors.Is(err, ErrInstallationNotFound) {
		t.Errorf("old org still resolves the installation, err = %v", err)
	}
	if _, err := s.FindGitHubInstallation(ctx, "org_new", "acme"); err != nil {
		t.Errorf("new org cannot resolve the installation: %v", err)
	}
}

func TestDeleteGitHubInstallation(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	_ = s.UpsertGitHubInstallation(ctx, &GitHubInstallation{
		InstallationID: 900, OrgID: "org_a", AccountLogin: "acme",
	})
	if err := s.DeleteGitHubInstallation(ctx, 900); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := s.FindGitHubInstallation(ctx, "org_a", "acme"); !errors.Is(err, ErrInstallationNotFound) {
		t.Errorf("installation survived deletion, err = %v", err)
	}
}

func TestUpsertGitHubInstallationValidation(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	cases := []struct {
		name string
		inst *GitHubInstallation
	}{
		{"nil", nil},
		{"no installation id", &GitHubInstallation{OrgID: "o", AccountLogin: "a"}},
		{"no org", &GitHubInstallation{InstallationID: 1, AccountLogin: "a"}},
		{"no account", &GitHubInstallation{InstallationID: 1, OrgID: "o"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := s.UpsertGitHubInstallation(ctx, tc.inst); err == nil {
				t.Error("want an error, got nil")
			}
		})
	}
}
