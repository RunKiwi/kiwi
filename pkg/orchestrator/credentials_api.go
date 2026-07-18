package orchestrator

import (
	"encoding/json"
	"net/http"
	"regexp"

	"github.com/ibreakthecloud/kiwi/pkg/auth"
)

var credNameRegex = regexp.MustCompile(`^[A-Z0-9_]+$`)

type setCredentialReq struct {
	Name  string `json:"name"`
	Kind  string `json:"kind"`
	Value string `json:"value"`
}

func (s *Server) handleSetCredential(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	claims := auth.ClaimsFromContext(r.Context())
	if claims == nil {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}

	var req setCredentialReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON body", http.StatusBadRequest)
		return
	}

	if req.Name == "" || req.Value == "" {
		http.Error(w, "name and value are required", http.StatusBadRequest)
		return
	}

	if !credNameRegex.MatchString(req.Name) {
		http.Error(w, "invalid name format: must match ^[A-Z0-9_]+$", http.StatusBadRequest)
		return
	}

	err := s.storage.SaveCredential(r.Context(), claims.OrgID, req.Name, req.Kind, req.Value)
	if err != nil {
		http.Error(w, "Failed to save credential", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
