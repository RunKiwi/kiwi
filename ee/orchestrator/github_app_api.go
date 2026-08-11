// SPDX-License-Identifier: LicenseRef-Kiwi-BSL-1.1
// Copyright (c) 2026 RunKiwi. Licensed under the Business Source License 1.1.
// See ee/LICENSE. This is Control Plane code and is NOT Apache-2.0.

package orchestrator

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/ibreakthecloud/kiwi/ee/githubapp"
	"github.com/ibreakthecloud/kiwi/pkg/daemon"
	"github.com/ibreakthecloud/kiwi/pkg/gitcache"
	"github.com/ibreakthecloud/kiwi/pkg/store"
)

// newGitHubAppClient builds the App client from the environment, or returns nil
// when no App is configured.
//
// nil is the ordinary case, not an error. Until an App exists every org
// authenticates with a stored GIT_TOKEN exactly as before, and the git-token
// endpoint answers "no installation" so the daemon falls back. That is what
// makes this change inert on deployment rather than a flag day.
func newGitHubAppClient() *githubapp.Client {
	appID := strings.TrimSpace(os.Getenv("KIWI_GITHUB_APP_ID"))
	rawKey := strings.TrimSpace(os.Getenv("KIWI_GITHUB_APP_PRIVATE_KEY"))
	if appID == "" || rawKey == "" {
		return nil
	}

	// A PEM carries newlines, which survive a Secret Manager mount but not
	// every path that reaches an env var. Accept base64 too rather than leave
	// operators debugging a key that looks present and parses as garbage.
	pemBytes := []byte(rawKey)
	if decoded, err := base64.StdEncoding.DecodeString(rawKey); err == nil && len(decoded) > 0 {
		pemBytes = decoded
	}

	client, err := githubapp.New(appID, pemBytes)
	if err != nil {
		// Loud, and non-fatal. A malformed key must not take down a control
		// plane whose other work is unaffected, but it must not fail silently
		// either: every App-backed task would otherwise fall back to a PAT that
		// may not exist and report a git error naming nothing.
		log.Printf("[githubapp] KIWI_GITHUB_APP_ID is set but the key is unusable, falling back to GIT_TOKEN: %v", err)
		return nil
	}
	log.Printf("[githubapp] GitHub App %s configured", appID)
	return client
}

// handleDaemonGitToken serves POST /api/v1/daemon/git-token.
//
// A daemon exchanges the lease it holds for a short-lived credential to the
// repository that task is against. Three things are checked, and the order
// matters because each narrows what the next one can reach:
//
//  1. the body is signed by a registered daemon's key;
//  2. that daemon holds the task's current lease, unexpired;
//  3. the repository comes from the task's own spec, never from the request.
//
// Together these mean a daemon can only ever buy a token for work it is
// actually running. Naming the repository in the request would have let any
// registered daemon mint against any repository its org had installed.
func (s *Server) handleDaemonGitToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req daemon.GitTokenReq
	_, _, err := readSignedBody(r, func(b []byte) (string, error) {
		if err := json.Unmarshal(b, &req); err != nil {
			return "", errors.New("invalid request body")
		}
		return req.SignPubKey, nil
	})
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	if req.TaskID == "" || req.LeaseID == "" {
		http.Error(w, "task_id and lease_id required", http.StatusBadRequest)
		return
	}

	d, err := s.storage.GetDaemonBySignPubKey(r.Context(), req.SignPubKey)
	if err != nil {
		if errors.Is(err, store.ErrDaemonNotFound) {
			http.Error(w, "daemon not registered", http.StatusForbidden)
			return
		}
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	task, err := s.storage.FindLeasedTask(r.Context(), req.TaskID, req.LeaseID)
	if err != nil {
		if errors.Is(err, store.ErrLeaseNotHeld) {
			http.Error(w, "lease not held", http.StatusConflict)
			return
		}
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	// Defence in depth. The fencing token already proves ownership, but a lease
	// held by a different daemon than the one signing means something is wrong
	// enough to refuse rather than reason about.
	if task.LeasedBy == nil || *task.LeasedBy != d.ID {
		http.Error(w, "lease not held", http.StatusConflict)
		return
	}

	repo, ok := gitcache.ParseRepo(specString(task.Spec, "repo_url"))
	if !ok || !repo.IsGitHub() {
		// Not a GitHub remote, so no App can serve it. The daemon falls back to
		// its sealed GIT_TOKEN, which is how GitLab and self-hosted keep working.
		http.Error(w, "no installation", http.StatusNotFound)
		return
	}

	if s.githubApp == nil {
		http.Error(w, "no installation", http.StatusNotFound)
		return
	}

	inst, err := s.storage.FindGitHubInstallation(r.Context(), task.OrgID, repo.Owner)
	if err != nil {
		if errors.Is(err, store.ErrInstallationNotFound) {
			http.Error(w, "no installation", http.StatusNotFound)
			return
		}
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	tok, err := s.githubApp.InstallationToken(r.Context(), inst.InstallationID)
	if err != nil {
		switch {
		case errors.Is(err, githubapp.ErrInstallationGone):
			// The customer uninstalled or dropped the repository. Drop the row
			// so the next submit fails fast with "connect GitHub" rather than
			// routing every task away from the PAT fallback into this error.
			if delErr := s.storage.DeleteGitHubInstallation(r.Context(), inst.InstallationID); delErr != nil {
				log.Printf("[githubapp] clearing revoked installation %d: %v", inst.InstallationID, delErr)
			}
			http.Error(w, "github app access was revoked for "+repo.Owner+"/"+repo.Name, http.StatusGone)
		case errors.Is(err, githubapp.ErrAppAuth):
			// Kiwi's own credentials, not this customer's. Affects every org.
			log.Printf("[githubapp] app authentication rejected; check KIWI_GITHUB_APP_ID and the private key: %v", err)
			http.Error(w, "github app misconfigured", http.StatusBadGateway)
		default:
			log.Printf("[githubapp] mint for installation %d: %v", inst.InstallationID, err)
			http.Error(w, "could not mint installation token", http.StatusBadGateway)
		}
		return
	}

	writeJSON(w, http.StatusOK, daemon.GitTokenResp{Token: tok.Value, ExpiresAt: tok.ExpiresAt})
}
