package orchestrator

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/ibreakthecloud/kiwi/pkg/auth"
)

// handleFleets serves GET (list) and POST (create) /api/v1/fleets.
func (s *Server) handleFleets(w http.ResponseWriter, r *http.Request) {
	claims := auth.ClaimsFromContext(r.Context())
	if claims == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	switch r.Method {
	case http.MethodGet:
		fleets, err := s.storage.ListFleets(r.Context(), claims.OrgID)
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"fleets": fleets})
	case http.MethodPost:
		var org auth.Organization
		if err := s.db.First(&org, "id = ?", claims.OrgID).Error; err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		if org.Plan == "free" {
			http.Error(w, "Free plan cannot create fleets. Upgrade to Pro for a dedicated fleet.", http.StatusForbidden)
			return
		}

		var body struct {
			Name string `json:"name"`
			Type string `json:"type"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.Name) == "" {
			http.Error(w, "name is required", http.StatusBadRequest)
			return
		}
		f, err := s.storage.CreateFleet(r.Context(), claims.OrgID, body.Name, body.Type)
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusCreated, f)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleModels serves GET (list) and POST (create) /api/v1/models, and
// DELETE /api/v1/models/{id}.
func (s *Server) handleModels(w http.ResponseWriter, r *http.Request) {
	claims := auth.ClaimsFromContext(r.Context())
	if claims == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	// DELETE /api/v1/models/{id}
	if r.Method == http.MethodDelete {
		id := strings.TrimPrefix(r.URL.Path, "/api/v1/models/")
		if id == "" || strings.Contains(id, "/") {
			http.Error(w, "model id required", http.StatusBadRequest)
			return
		}
		if err := s.storage.DeleteModel(r.Context(), claims.OrgID, id); err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}
	switch r.Method {
	case http.MethodGet:
		models, err := s.storage.ListModels(r.Context(), claims.OrgID)
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"models": models})
	case http.MethodPost:
		var body struct {
			Name     string `json:"name"`
			Provider string `json:"provider"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.Name) == "" {
			http.Error(w, "name is required", http.StatusBadRequest)
			return
		}
		if body.Provider == "" {
			body.Provider = inferProvider(body.Name)
		}
		m, err := s.storage.CreateModel(r.Context(), claims.OrgID, body.Name, body.Provider)
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusCreated, m)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// inferProvider guesses the provider from the model id (matches daemon routing).
func inferProvider(model string) string {
	if strings.HasPrefix(model, "gemini") {
		return "gemini"
	}
	if strings.HasPrefix(model, "claude") {
		return "anthropic"
	}
	return "anthropic"
}

// integrationSpec maps an integration key to the credential name that backs it.
var integrationSpec = []struct {
	Key      string `json:"key"`
	CredName string `json:"-"`
	Kind     string `json:"kind"`
}{
	{"github", "GITHUB_TOKEN", "github"},
	{"slack", "SLACK_TOKEN", "slack"},
	{"anthropic", "ANTHROPIC_API_KEY", "llm"},
	{"gemini", "GEMINI_API_KEY", "llm"},
	{"git", "GIT_TOKEN", "git"},
}

// handleIntegrations serves GET /api/v1/integrations — which integrations are
// connected, derived from which credential names exist (values never returned).
func (s *Server) handleIntegrations(w http.ResponseWriter, r *http.Request) {
	claims := auth.ClaimsFromContext(r.Context())
	if claims == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	creds, err := s.storage.ListCredentials(r.Context(), claims.OrgID)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	present := make(map[string]bool, len(creds))
	for _, c := range creds {
		present[c.Name] = true
	}
	type item struct {
		Key       string `json:"key"`
		Kind      string `json:"kind"`
		Connected bool   `json:"connected"`
	}
	out := make([]item, 0, len(integrationSpec))
	for _, spec := range integrationSpec {
		out = append(out, item{Key: spec.Key, Kind: spec.Kind, Connected: present[spec.CredName]})
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"integrations": out})
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
