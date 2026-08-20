// ee/orchestrator/slack_bindings_api.go
// SPDX-License-Identifier: LicenseRef-Kiwi-BSL-1.1
// Copyright (c) 2026 RunKiwi. Licensed under the Business Source License 1.1.
// See ee/LICENSE. This is Control Plane code and is NOT Apache-2.0.

package orchestrator

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/ibreakthecloud/kiwi/ee/auth"
	"github.com/ibreakthecloud/kiwi/pkg/store"
)

// handleSlackBindings serves POST and GET /api/v1/integrations/slack/bindings.
func (s *Server) handleSlackBindings(w http.ResponseWriter, r *http.Request) {
	claims := auth.ClaimsFromContext(r.Context())
	if claims == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	switch r.Method {
	case http.MethodPost:
		var body struct {
			TeamID         string `json:"team_id"`
			ChannelID      string `json:"channel_id"`
			RepoURL        string `json:"repo_url"`
			DefaultTestCmd string `json:"default_test_cmd"`
			DefaultRef     string `json:"default_ref"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		if body.TeamID == "" || body.ChannelID == "" || body.RepoURL == "" {
			http.Error(w, "team_id, channel_id, and repo_url are required", http.StatusBadRequest)
			return
		}
		b := &store.SlackChannelBinding{
			OrgID: claims.OrgID, TeamID: body.TeamID, ChannelID: body.ChannelID,
			RepoURL: body.RepoURL, DefaultTestCmd: body.DefaultTestCmd, DefaultRef: body.DefaultRef,
			CreatedBy: claims.UserID,
		}
		if err := s.storage.CreateSlackChannelBinding(r.Context(), b); err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusCreated, b)

	case http.MethodGet:
		list, err := s.storage.ListSlackChannelBindings(r.Context(), claims.OrgID)
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		if list == nil {
			list = []store.SlackChannelBinding{}
		}
		writeJSON(w, http.StatusOK, map[string]any{"bindings": list})

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleDeleteSlackBinding serves DELETE /api/v1/integrations/slack/bindings/{id}.
func (s *Server) handleDeleteSlackBinding(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	claims := auth.ClaimsFromContext(r.Context())
	if claims == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/api/v1/integrations/slack/bindings/")
	if id == "" {
		http.Error(w, "missing binding id", http.StatusBadRequest)
		return
	}
	if err := s.storage.DeleteSlackChannelBinding(r.Context(), id, claims.OrgID); err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
