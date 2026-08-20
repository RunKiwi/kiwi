// ee/orchestrator/slack_install_api.go
// SPDX-License-Identifier: LicenseRef-Kiwi-BSL-1.1
// Copyright (c) 2026 RunKiwi. Licensed under the Business Source License 1.1.
// See ee/LICENSE. This is Control Plane code and is NOT Apache-2.0.

package orchestrator

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/ibreakthecloud/kiwi/ee/auth"
	"github.com/ibreakthecloud/kiwi/pkg/store"
)

// slackScopes is the fixed set of bot scopes this integration asks for:
// posting/editing messages, reacting, reading channel and thread history,
// and receiving app_mention events. Kept as one constant rather than
// per-install configuration — every workspace gets the same bot.
const slackScopes = "app_mentions:read,chat:write,reactions:write,reactions:read,channels:history,groups:history,im:history"

func slackRedirectURI() string {
	return strings.TrimRight(dashboardAPIBaseURL(), "/") + "/api/v1/integrations/slack/oauth/callback"
}

// dashboardAPIBaseURL is the Control Plane's own public URL — reuse
// whatever env var github_install_api.go / server.go already reads for
// this (e.g. KIWI_API_BASE_URL); do not introduce a second name for the
// same concept. Adjust this helper to call that existing one directly if
// it already exists under a different name.
func dashboardAPIBaseURL() string {
	return strings.TrimRight(os.Getenv("KIWI_API_BASE_URL"), "/")
}

// handleSlackInstall serves GET /api/v1/integrations/slack/install,
// mirroring handleGithubInstall (github_install_api.go) exactly: the org is
// taken from the caller's own credentials and sealed into the signed state,
// never read back from Slack's redirect.
func (s *Server) handleSlackInstall(w http.ResponseWriter, r *http.Request) {
	claims := auth.ClaimsFromContext(r.Context())
	if claims == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	clientID := strings.TrimSpace(os.Getenv("KIWI_SLACK_CLIENT_ID"))
	if clientID == "" {
		http.Error(w, "slack app is not configured", http.StatusNotImplemented)
		return
	}

	nonce := make([]byte, 16)
	if _, err := rand.Read(nonce); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	state, err := signInstallState(installState{
		OrgID:     claims.OrgID,
		UserID:    claims.UserID,
		Nonce:     hex.EncodeToString(nonce),
		ExpiresAt: time.Now().Add(installStateTTL).Unix(),
	})
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	target := fmt.Sprintf("https://slack.com/oauth/v2/authorize?client_id=%s&scope=%s&redirect_uri=%s&state=%s",
		url.QueryEscape(clientID), url.QueryEscape(slackScopes), url.QueryEscape(slackRedirectURI()), url.QueryEscape(state))

	if strings.Contains(r.Header.Get("Accept"), "application/json") {
		writeJSON(w, http.StatusOK, map[string]string{"install_url": target})
		return
	}
	http.Redirect(w, r, target, http.StatusFound)
}

// handleSlackOAuthCallback serves GET /api/v1/integrations/slack/oauth/callback.
// Unauthenticated on purpose, exactly like handleGithubCallback: the signed
// state is the credential proving which org started this install.
func (s *Server) handleSlackOAuthCallback(w http.ResponseWriter, r *http.Request) {
	st, err := verifyInstallState(r.URL.Query().Get("state"))
	if err != nil {
		log.Printf("[slackapp] install callback rejected: %v", err)
		http.Error(w, "invalid or expired install link", http.StatusBadRequest)
		return
	}

	code := r.URL.Query().Get("code")
	if code == "" {
		http.Error(w, "missing code", http.StatusBadRequest)
		return
	}

	clientID := os.Getenv("KIWI_SLACK_CLIENT_ID")
	clientSecret := os.Getenv("KIWI_SLACK_CLIENT_SECRET")
	if clientID == "" || clientSecret == "" || s.slackClient == nil {
		http.Error(w, "slack app is not configured", http.StatusNotImplemented)
		return
	}

	result, err := s.slackClient.ExchangeOAuthCode(r.Context(), clientID, clientSecret, code, slackRedirectURI())
	if err != nil {
		log.Printf("[slackapp] oauth exchange failed: %v", err)
		http.Error(w, "could not complete the Slack install", http.StatusBadGateway)
		return
	}

	if err := s.storage.SaveCredential(r.Context(), st.OrgID, "SLACK_BOT_TOKEN", store.CredentialSlack, result.AccessToken); err != nil {
		log.Printf("[slackapp] persist bot token for %s: %v", st.OrgID, err)
		http.Error(w, "could not save the installation", http.StatusInternalServerError)
		return
	}
	if err := s.storage.UpsertSlackInstallation(r.Context(), &store.SlackInstallation{
		TeamID: result.TeamID, OrgID: st.OrgID, TeamName: result.TeamName, InstalledByUserID: st.UserID,
	}); err != nil {
		log.Printf("[slackapp] persist installation for %s: %v", st.OrgID, err)
		http.Error(w, "could not save the installation", http.StatusInternalServerError)
		return
	}

	log.Printf("[slackapp] org %s connected Slack workspace %s (team %s)", st.OrgID, result.TeamName, result.TeamID)
	http.Redirect(w, r, dashboardURL()+"/integrations?slack=connected", http.StatusFound)
}

// handleSlackInstallations serves GET /api/v1/integrations/slack/installations
// so the dashboard can show what's connected.
func (s *Server) handleSlackInstallations(w http.ResponseWriter, r *http.Request) {
	claims := auth.ClaimsFromContext(r.Context())
	if claims == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	list, err := s.storage.ListSlackInstallations(r.Context(), claims.OrgID)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if list == nil {
		list = []store.SlackInstallation{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"installations": list})
}
