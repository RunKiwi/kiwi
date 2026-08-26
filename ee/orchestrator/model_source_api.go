// ee/orchestrator/model_source_api.go
// SPDX-License-Identifier: LicenseRef-Kiwi-BSL-1.1
// Copyright (c) 2026 RunKiwi. Licensed under the Business Source License 1.1.
// See ee/LICENSE. This is Control Plane code and is NOT Apache-2.0.

package orchestrator

import (
	"encoding/json"
	"net/http"

	"github.com/ibreakthecloud/kiwi/ee/auth"
	"github.com/ibreakthecloud/kiwi/pkg/store"
)

// handleModelSource serves GET and PATCH /api/v1/org/model-source — the
// org's default_model_source preference that ee/planner's
// defaultWorkerModelFor consults for every submit that doesn't name a
// worker model explicitly (a channel-unbound Slack trigger, the common
// case, but also a bare CLI/dashboard submit).
func (s *Server) handleModelSource(w http.ResponseWriter, r *http.Request) {
	claims := auth.ClaimsFromContext(r.Context())
	if claims == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	switch r.Method {
	case http.MethodGet:
		source, err := s.storage.ModelSource(r.Context(), claims.OrgID)
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"default_model_source": source})

	case http.MethodPatch:
		var body struct {
			DefaultModelSource string `json:"default_model_source"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		if body.DefaultModelSource != store.ModelSourceKiwi && body.DefaultModelSource != store.ModelSourceBYOK {
			http.Error(w, "default_model_source must be \"kiwi\" or \"byok\"", http.StatusBadRequest)
			return
		}
		if err := s.storage.SetModelSource(r.Context(), claims.OrgID, body.DefaultModelSource); err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"default_model_source": body.DefaultModelSource})

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}
