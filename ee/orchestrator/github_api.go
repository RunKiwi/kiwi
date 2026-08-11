// SPDX-License-Identifier: LicenseRef-Kiwi-BSL-1.1
// Copyright (c) 2026 RunKiwi. Licensed under the Business Source License 1.1.
// See ee/LICENSE. This is Control Plane code and is NOT Apache-2.0.

package orchestrator

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"sort"
	"time"

	"github.com/ibreakthecloud/kiwi/ee/auth"
	"github.com/ibreakthecloud/kiwi/ee/githubapp"
)

// repo is what the task form needs to offer a repository.
type repo struct {
	FullName      string `json:"full_name"`
	URL           string `json:"url"`
	Private       bool   `json:"private"`
	DefaultBranch string `json:"default_branch"`
}

// handleGithubRepos serves GET /api/v1/github/repos — the repositories the task
// form can offer.
//
// Prefers the App. An installation lists exactly what the customer ticked,
// whereas a personal access token lists everything its owner can see, most of
// which Kiwi cannot act on. Sourcing the picker from the installation makes the
// dropdown and the permission agree, so a repository that appears in it is one
// a task can actually run against.
//
// Falls back to the stored GITHUB_TOKEN, which is what keeps the picker working
// for orgs that have not installed the App and for deployments with no App
// configured at all.
func (s *Server) handleGithubRepos(w http.ResponseWriter, r *http.Request) {
	claims := auth.ClaimsFromContext(r.Context())
	if claims == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	if repos, ok := s.reposFromInstallations(r, claims.OrgID); ok {
		writeJSON(w, http.StatusOK, map[string]interface{}{"repos": repos})
		return
	}

	token, err := s.storage.GetCredentialPlaintext(r.Context(), claims.OrgID, "GITHUB_TOKEN")
	if err != nil || token == "" {
		http.Error(w, "GitHub not connected — install the Kiwi GitHub App, or add a token under Integrations", http.StatusPreconditionRequired)
		return
	}

	req, _ := http.NewRequestWithContext(r.Context(), http.MethodGet,
		"https://api.github.com/user/repos?per_page=100&sort=updated&affiliation=owner,collaborator,organization_member", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		http.Error(w, "failed to reach GitHub", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		http.Error(w, fmt.Sprintf("GitHub returned %d", resp.StatusCode), http.StatusBadGateway)
		return
	}

	var ghRepos []struct {
		FullName      string `json:"full_name"`
		HTMLURL       string `json:"html_url"`
		Private       bool   `json:"private"`
		DefaultBranch string `json:"default_branch"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&ghRepos); err != nil {
		http.Error(w, "failed to parse GitHub response", http.StatusBadGateway)
		return
	}

	repos := make([]repo, 0, len(ghRepos))
	for _, g := range ghRepos {
		repos = append(repos, repo{FullName: g.FullName, URL: g.HTMLURL, Private: g.Private, DefaultBranch: g.DefaultBranch})
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"repos": repos})
}

// reposFromInstallations gathers repositories across every account this org has
// connected. ok is false when the App cannot answer at all, which is the signal
// to fall back to the stored token.
//
// A single installation failing does not fail the request. An org can connect a
// personal account and a company org; one being revoked should not empty the
// picker for the other, and the revoked one is cleared as it is discovered so
// the state converges without anyone running a task.
func (s *Server) reposFromInstallations(r *http.Request, orgID string) ([]repo, bool) {
	if s.githubApp == nil {
		return nil, false
	}
	installs, err := s.storage.ListGitHubInstallations(r.Context(), orgID)
	if err != nil || len(installs) == 0 {
		return nil, false
	}

	out := make([]repo, 0, 32)
	served := false
	for _, inst := range installs {
		list, err := s.githubApp.ListRepositories(r.Context(), inst.InstallationID)
		if err != nil {
			if errors.Is(err, githubapp.ErrInstallationGone) {
				if delErr := s.storage.DeleteGitHubInstallation(r.Context(), inst.InstallationID); delErr != nil {
					log.Printf("[githubapp] clearing revoked installation %d: %v", inst.InstallationID, delErr)
				}
			} else {
				log.Printf("[githubapp] listing repositories for installation %d: %v", inst.InstallationID, err)
			}
			continue
		}
		served = true
		for _, g := range list {
			out = append(out, repo{
				FullName:      g.FullName,
				URL:           g.HTMLURL,
				Private:       g.Private,
				DefaultBranch: g.DefaultBranch,
			})
		}
	}
	if !served {
		return nil, false
	}

	sort.Slice(out, func(i, j int) bool { return out[i].FullName < out[j].FullName })
	return out, true
}
