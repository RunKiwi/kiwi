// SPDX-License-Identifier: LicenseRef-Kiwi-BSL-1.1
// Copyright (c) 2026 RunKiwi. Licensed under the Business Source License 1.1.
// See ee/LICENSE. This is Control Plane code and is NOT Apache-2.0.

package planner

import (
	"context"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"github.com/ibreakthecloud/kiwi/pkg/store"
)

func repoAuthStore(t *testing.T) *store.PostgresStore {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&store.Organization{}, &store.Credential{}, &store.GitHubInstallation{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return store.NewPostgresStore(db)
}

func TestRequireRepoAuthAcceptsInstallation(t *testing.T) {
	st := repoAuthStore(t)
	ctx := context.Background()

	if err := st.UpsertGitHubInstallation(ctx, &store.GitHubInstallation{
		InstallationID: 1, OrgID: "org_a", AccountLogin: "RunKiwi",
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if err := requireRepoAuth(ctx, st, "org_a", "https://github.com/RunKiwi/kiwi.git"); err != nil {
		t.Fatalf("rejected a repo covered by an installation: %v", err)
	}
}

// An org that never installed the App keeps working on its PAT.
func TestRequireRepoAuthAcceptsStoredToken(t *testing.T) {
	st := repoAuthStore(t)
	ctx := context.Background()

	if err := st.SaveCredential(ctx, "org_a", "GIT_TOKEN", "git", "ghp_pat"); err != nil {
		t.Fatalf("seed credential: %v", err)
	}
	if err := requireRepoAuth(ctx, st, "org_a", "https://github.com/Someone/else.git"); err != nil {
		t.Fatalf("rejected a repo with a stored GIT_TOKEN: %v", err)
	}
}

// The case this exists for. Without it the task is accepted, leased, and fails
// twenty minutes later with a git error naming no credential.
func TestRequireRepoAuthRejectsWhenNothingCanReachIt(t *testing.T) {
	st := repoAuthStore(t)

	err := requireRepoAuth(context.Background(), st, "org_a", "https://github.com/RunKiwi/kiwi.git")
	if err == nil {
		t.Fatal("accepted a task with no auth path")
	}
	// The message has to say what to do. "failed to provision worktree" is what
	// this replaces.
	if !strings.Contains(err.Error(), "install the Kiwi GitHub App") {
		t.Errorf("error does not name the fix: %v", err)
	}
	if !strings.Contains(err.Error(), "RunKiwi") {
		t.Errorf("error does not name the repository: %v", err)
	}
}

// One org's installation must not make another org's submission look reachable.
func TestRequireRepoAuthIsOrgScoped(t *testing.T) {
	st := repoAuthStore(t)
	ctx := context.Background()

	if err := st.UpsertGitHubInstallation(ctx, &store.GitHubInstallation{
		InstallationID: 1, OrgID: "org_b", AccountLogin: "RunKiwi",
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := requireRepoAuth(ctx, st, "org_a", "https://github.com/RunKiwi/kiwi.git"); err == nil {
		t.Fatal("another org's installation satisfied this org's submission")
	}
}

// The App covers github.com only, so a GitLab remote needs the token and the
// message must not send someone off to install an App that cannot help.
func TestRequireRepoAuthNonGitHubNeedsToken(t *testing.T) {
	st := repoAuthStore(t)
	ctx := context.Background()

	err := requireRepoAuth(ctx, st, "org_a", "https://gitlab.com/acme/widgets.git")
	if err == nil {
		t.Fatal("accepted a GitLab repo with no token")
	}
	if strings.Contains(err.Error(), "install the Kiwi GitHub App") {
		t.Errorf("suggested the GitHub App for a GitLab remote: %v", err)
	}

	if err := st.SaveCredential(ctx, "org_a", "GIT_TOKEN", "git", "glpat"); err != nil {
		t.Fatalf("seed credential: %v", err)
	}
	if err := requireRepoAuth(ctx, st, "org_a", "https://gitlab.com/acme/widgets.git"); err != nil {
		t.Fatalf("rejected a GitLab repo with a stored token: %v", err)
	}
}

func TestRequireRepoAuthRejectsUnparseableRemote(t *testing.T) {
	st := repoAuthStore(t)
	if err := requireRepoAuth(context.Background(), st, "org_a", "not a git remote"); err == nil {
		t.Fatal("accepted an unparseable remote")
	}
}

// Submission paths that carry no repository are not this check's business.
func TestRequireRepoAuthIgnoresEmptyRepo(t *testing.T) {
	st := repoAuthStore(t)
	if err := requireRepoAuth(context.Background(), st, "org_a", ""); err != nil {
		t.Fatalf("rejected a submission with no repo: %v", err)
	}
}
