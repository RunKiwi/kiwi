// SPDX-License-Identifier: LicenseRef-Kiwi-BSL-1.1
// Copyright (c) 2026 RunKiwi. Licensed under the Business Source License 1.1.
// See ee/LICENSE. This is Control Plane code and is NOT Apache-2.0.

package planner

import (
	"context"
	"errors"
	"fmt"

	"github.com/ibreakthecloud/kiwi/pkg/gitcache"
	"github.com/ibreakthecloud/kiwi/pkg/store"
)

// requireRepoAuth refuses a submission whose repository nothing can reach.
//
// Rejecting here rather than at execution is the whole point. A task accepted
// without an auth path is leased, provisions a workspace, fails to clone, and
// reports a git error that names no credential — twenty minutes after the
// person who could have fixed it in ten seconds walked away. The same lesson as
// the queue's "accepted with no runner" case: do not take work that cannot run.
//
// Two credentials can serve, and the order matches the daemon's own resolution
// so this check and the eventual attempt cannot disagree: a GitHub App
// installation covering the repository's owner, or a stored GIT_TOKEN.
func requireRepoAuth(ctx context.Context, st store.Store, orgID, repoURL string) error {
	if repoURL == "" {
		// Not every submission path carries a repository. Those that do not are
		// not this function's business.
		return nil
	}

	repo, ok := gitcache.ParseRepo(repoURL)
	if !ok {
		return fmt.Errorf("could not parse %q as a git remote: expected https://host/owner/name or git@host:owner/name", repoURL)
	}

	if repo.IsGitHub() {
		if _, err := st.FindGitHubInstallation(ctx, orgID, repo.Owner); err == nil {
			return nil
		} else if !errors.Is(err, store.ErrInstallationNotFound) {
			// A lookup that failed for some other reason is not proof of
			// absence, and refusing the task on a transient database error
			// would be worse than letting it try.
			return nil
		}
	}

	token, err := st.GetCredentialPlaintext(ctx, orgID, "GIT_TOKEN")
	if err == nil && token != "" {
		return nil
	}

	if repo.IsGitHub() {
		return fmt.Errorf(
			"no access to github.com/%s/%s: install the Kiwi GitHub App on %s, or add a GIT_TOKEN under Integrations",
			repo.Owner, repo.Name, repo.Owner)
	}
	return fmt.Errorf(
		"no access to %s/%s on %s: add a GIT_TOKEN under Integrations (the GitHub App covers github.com only)",
		repo.Owner, repo.Name, repo.Host)
}
