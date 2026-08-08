package orchestrator

import (
	"net/http"

	"github.com/ibreakthecloud/kiwi/pkg/auth"
	"github.com/ibreakthecloud/kiwi/pkg/provider"
)

// handleProviders serves GET /api/v1/providers.
//
// It exists so the frontend stops keeping its own copy of the provider list and
// its own model-to-provider rule. One definition, served to every consumer.
//
// Key material never appears in the response: kiwi_available reports only
// whether a platform key is configured, never what it is.
func (s *Server) handleProviders(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
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
		ID            string `json:"id"`
		Display       string `json:"display"`
		Kind          string `json:"kind"`
		Connected     bool   `json:"connected"`
		KiwiAvailable bool   `json:"kiwi_available"`
	}
	specs := provider.Registry()
	out := make([]item, 0, len(specs))
	for _, spec := range specs {
		_, kiwiAvailable := provider.PlatformKeyFor(spec.ID)
		out = append(out, item{
			ID:            spec.ID,
			Display:       spec.Display,
			Kind:          spec.Kind,
			Connected:     present[spec.CredName],
			KiwiAvailable: kiwiAvailable,
		})
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"providers": out})
}

// handleCatalogModels serves GET /api/v1/catalog/models — the discovered models
// available to this org, global plus its own.
func (s *Server) handleCatalogModels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	claims := auth.ClaimsFromContext(r.Context())
	if claims == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	models, err := s.storage.ListCatalogModels(r.Context(), claims.OrgID)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	// Only selectable models reach the picker. The rest stay in the table for
	// pricing history and for the admin view.
	out := make([]interface{}, 0, len(models))
	for _, m := range models {
		if m.Selectable {
			out = append(out, m)
		}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"models": out})
}
