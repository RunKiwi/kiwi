// SPDX-License-Identifier: LicenseRef-Kiwi-BSL-1.1
// Copyright (c) 2026 RunKiwi. Licensed under the Business Source License 1.1.
// See ee/LICENSE. This is Control Plane code and is NOT Apache-2.0.

package orchestrator

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/ibreakthecloud/kiwi/ee/auth"
	"github.com/ibreakthecloud/kiwi/ee/githubapp"
	"github.com/ibreakthecloud/kiwi/pkg/store"
)

// installStateTTL bounds how long a started install may take to come back.
//
// Long enough to read a consent screen and pick repositories, short enough that
// a state captured from a browser history or a shared screen is dead before it
// is useful.
const installStateTTL = 15 * time.Minute

// installState is what survives the round trip to GitHub.
//
// GitHub's redirect carries an installation id and whatever opaque state was
// sent, and nothing else. The org therefore has to travel in the state, and the
// state has to be signed: an unsigned one would let anyone bind their own
// GitHub account to another tenant's org simply by editing a query parameter.
type installState struct {
	OrgID     string `json:"org_id"`
	UserID    string `json:"user_id"`
	Nonce     string `json:"nonce"`
	ExpiresAt int64  `json:"expires_at"`
}

func installStateSecret() []byte {
	secret := os.Getenv("KIWI_SESSION_SECRET")
	if secret == "" {
		// Matches the session cookie's fallback so behaviour is consistent in
		// local dev. Production sets the variable; a deployment that does not
		// has a much larger problem than this endpoint.
		secret = "default-insecure-secret"
	}
	return []byte(secret)
}

func signInstallState(st installState) (string, error) {
	raw, err := json.Marshal(st)
	if err != nil {
		return "", err
	}
	data := base64.RawURLEncoding.EncodeToString(raw)
	mac := hmac.New(sha256.New, installStateSecret())
	mac.Write([]byte(data))
	return data + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

func verifyInstallState(value string) (installState, error) {
	parts := strings.Split(value, ".")
	if len(parts) != 2 {
		return installState{}, errors.New("malformed state")
	}
	mac := hmac.New(sha256.New, installStateSecret())
	mac.Write([]byte(parts[0]))
	want := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(parts[1]), []byte(want)) {
		return installState{}, errors.New("bad state signature")
	}

	raw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return installState{}, errors.New("malformed state payload")
	}
	var st installState
	if err := json.Unmarshal(raw, &st); err != nil {
		return installState{}, errors.New("malformed state payload")
	}
	if st.OrgID == "" {
		return installState{}, errors.New("state carries no org")
	}
	if time.Now().Unix() > st.ExpiresAt {
		return installState{}, errors.New("state expired")
	}
	return st, nil
}

// handleGithubInstall serves GET /api/v1/github/install.
//
// Authenticated: the org this install will be bound to is taken from the
// caller's own credentials and sealed into the state, never read back from
// GitHub's redirect.
func (s *Server) handleGithubInstall(w http.ResponseWriter, r *http.Request) {
	claims := auth.ClaimsFromContext(r.Context())
	if claims == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	slug := strings.TrimSpace(os.Getenv("KIWI_GITHUB_APP_SLUG"))
	if slug == "" || s.githubApp == nil {
		http.Error(w, "github app is not configured", http.StatusNotImplemented)
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

	target := fmt.Sprintf("https://github.com/apps/%s/installations/new?state=%s", slug, state)

	// Content-negotiated because the callers cannot both follow a redirect.
	//
	// The dashboard authenticates with a bearer token held in localStorage, and
	// a top-level browser navigation carries no Authorization header, so it
	// cannot simply point the window at this endpoint. It asks for JSON, gets
	// the URL, and navigates itself. The CLI has the same problem for a
	// different reason and the same answer.
	if strings.Contains(r.Header.Get("Accept"), "application/json") {
		writeJSON(w, http.StatusOK, map[string]string{"install_url": target})
		return
	}
	http.Redirect(w, r, target, http.StatusFound)
}

// handleGithubCallback serves GET /api/v1/github/callback, where GitHub sends
// the browser once the customer has chosen an account and its repositories.
//
// Unauthenticated on purpose. The signed state is the credential: it proves
// which org started this install, and it is the only thing here that a caller
// cannot forge. A session cookie would be a weaker basis, since the redirect
// arrives cross-site and the cookie may not.
func (s *Server) handleGithubCallback(w http.ResponseWriter, r *http.Request) {
	if s.githubApp == nil {
		http.Error(w, "github app is not configured", http.StatusNotImplemented)
		return
	}

	st, err := verifyInstallState(r.URL.Query().Get("state"))
	if err != nil {
		log.Printf("[githubapp] install callback rejected: %v", err)
		http.Error(w, "invalid or expired install link", http.StatusBadRequest)
		return
	}

	installationID, err := strconv.ParseInt(r.URL.Query().Get("installation_id"), 10, 64)
	if err != nil || installationID <= 0 {
		http.Error(w, "missing installation_id", http.StatusBadRequest)
		return
	}

	// Ask GitHub which account this is. The redirect does not say, and believing
	// a caller-supplied account would let a guessed id claim someone else's.
	inst, err := s.githubApp.GetInstallation(r.Context(), installationID)
	if err != nil {
		switch {
		case errors.Is(err, githubapp.ErrInstallationGone):
			http.Error(w, "that installation no longer exists", http.StatusBadRequest)
		case errors.Is(err, githubapp.ErrAppAuth):
			log.Printf("[githubapp] app authentication rejected during install: %v", err)
			http.Error(w, "github app misconfigured", http.StatusBadGateway)
		default:
			log.Printf("[githubapp] install lookup for %d: %v", installationID, err)
			http.Error(w, "could not confirm the installation", http.StatusBadGateway)
		}
		return
	}

	if err := s.storage.UpsertGitHubInstallation(r.Context(), &store.GitHubInstallation{
		InstallationID: inst.ID,
		OrgID:          st.OrgID,
		AccountLogin:   inst.Account.Login,
		RepoSelection:  inst.RepositorySelection,
	}); err != nil {
		log.Printf("[githubapp] persist installation %d for %s: %v", inst.ID, st.OrgID, err)
		http.Error(w, "could not save the installation", http.StatusInternalServerError)
		return
	}

	log.Printf("[githubapp] org %s connected GitHub account %s (installation %d, %s repos)",
		st.OrgID, inst.Account.Login, inst.ID, inst.RepositorySelection)

	http.Redirect(w, r, dashboardURL()+"/integrations?github=connected", http.StatusFound)
}

// handleGithubInstallations serves GET /api/v1/github/installations, so the
// dashboard can show what is connected and offer to disconnect it.
func (s *Server) handleGithubInstallations(w http.ResponseWriter, r *http.Request) {
	claims := auth.ClaimsFromContext(r.Context())
	if claims == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	list, err := s.storage.ListGitHubInstallations(r.Context(), claims.OrgID)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if list == nil {
		list = []store.GitHubInstallation{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"installations": list})
}

// dashboardURL is where the callback sends the browser once the install is
// recorded. Falls back to a relative path so local dev lands somewhere useful
// rather than on a hard-coded production host.
func dashboardURL() string {
	if v := strings.TrimRight(os.Getenv("KIWI_DASHBOARD_URL"), "/"); v != "" {
		return v
	}
	return ""
}
