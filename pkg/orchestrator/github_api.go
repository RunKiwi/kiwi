package orchestrator

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/ibreakthecloud/kiwi/pkg/auth"
)

// handleGithubRepos serves GET /api/v1/github/repos — lists the repositories
// the org's stored GitHub token can access, so the task form can offer them.
// The token stays server-side; only repo names/urls are returned.
func (s *Server) handleGithubRepos(w http.ResponseWriter, r *http.Request) {
	claims := auth.ClaimsFromContext(r.Context())
	if claims == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	token, err := s.storage.GetCredentialPlaintext(r.Context(), claims.OrgID, "GITHUB_TOKEN")
	if err != nil || token == "" {
		http.Error(w, "GitHub not connected — add a token under Integrations", http.StatusPreconditionRequired)
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

	type repo struct {
		FullName      string `json:"full_name"`
		URL           string `json:"url"`
		Private       bool   `json:"private"`
		DefaultBranch string `json:"default_branch"`
	}
	repos := make([]repo, 0, len(ghRepos))
	for _, g := range ghRepos {
		repos = append(repos, repo{FullName: g.FullName, URL: g.HTMLURL, Private: g.Private, DefaultBranch: g.DefaultBranch})
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"repos": repos})
}
