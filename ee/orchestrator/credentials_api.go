// SPDX-License-Identifier: LicenseRef-Kiwi-BSL-1.1
// Copyright (c) 2026 RunKiwi. Licensed under the Business Source License 1.1.
// See ee/LICENSE. This is Control Plane code and is NOT Apache-2.0.

package orchestrator

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"regexp"
	"time"

	"github.com/ibreakthecloud/kiwi/ee/auth"
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

	// Validate the credential with its provider before storing it, so a typo'd or
	// revoked key is rejected here instead of failing a task later. Fails open on
	// unknown names and transient errors (see defaultCredValidator).
	if s.credValidator != nil {
		if err := s.credValidator(r.Context(), req.Name, req.Value); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	}

	err := s.storage.SaveCredential(r.Context(), claims.OrgID, req.Name, req.Kind, req.Value)
	if err != nil {
		http.Error(w, "Failed to save credential", http.StatusInternalServerError)
		return
	}

	// Discover this provider's models now rather than at the next daily refresh:
	// someone who just pasted a key expects to see their models.
	//
	// Scoped to the provider that changed — refreshing all of them would make
	// unrelated API calls with unrelated keys on every save. Detached from the
	// request context and given its own deadline, because that context is
	// cancelled the moment the response is written, and holding the response
	// open for a third-party call would make saving a key as slow and as
	// failure-prone as the provider is.
	if s.refresher != nil {
		orgID, credName, credValue := claims.OrgID, req.Name, req.Value
		refresher := s.refresher
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			if err := refresher.RefreshOrgProvider(ctx, orgID, credName, credValue); err != nil {
				log.Printf("[catalog] on-save discovery for org %s (%s): %v", orgID, credName, err)
			}
		}()
	}

	w.WriteHeader(http.StatusNoContent)
}
