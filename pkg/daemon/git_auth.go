package daemon

import (
	"context"
	"encoding/base64"
	"errors"
	"log"
)

// resolveGitToken returns the credential git should authenticate with for one
// task, preferring a GitHub App installation token over the sealed GIT_TOKEN.
//
// The order is deliberate. An installation token is scoped to the repositories
// the customer actually granted and dies within the hour; GIT_TOKEN is whatever
// personal access token somebody pasted, usually carrying org-wide write and no
// expiry. Preferring the App means an org that connects GitHub stops relying on
// the weaker credential without anyone migrating anything.
//
// Falling back rather than failing is what keeps this change inert on
// deployment: non-GitHub remotes, orgs that never installed the App, and any
// control plane with no App configured all answer "no installation" and carry
// on exactly as before.
func (d *Daemon) resolveGitToken(ctx context.Context, taskID, leaseID string, creds map[string]string) (string, error) {
	pat := creds["GIT_TOKEN"]

	resp, err := d.client.GitToken(ctx, GitTokenReq{
		TaskID:     taskID,
		LeaseID:    leaseID,
		SignPubKey: base64.StdEncoding.EncodeToString(d.signPubKey),
	})
	switch {
	case err == nil:
		return resp.Token, nil

	case errors.Is(err, ErrNoInstallation):
		// The ordinary path before the App is rolled out, and permanently so
		// for GitLab, Bitbucket and self-hosted remotes.
		return pat, nil

	case errors.Is(err, ErrInstallationRevoked):
		// No fallback attempt here on purpose. An org that connected GitHub and
		// then revoked it almost certainly holds no PAT, and retrying with an
		// empty credential converts a clear, actionable message into
		// "could not read Username for 'https://github.com'".
		return "", err

	case errors.Is(err, ErrLeaseLost):
		return "", err

	default:
		// Transient: a control plane blip must not lose a task that has a
		// perfectly good PAT sitting in its sealed bundle.
		if pat != "" {
			log.Printf("[git-auth] installation token unavailable for %s, falling back to GIT_TOKEN: %v", taskID, err)
			return pat, nil
		}
		return "", err
	}
}
