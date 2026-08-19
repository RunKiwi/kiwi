// SPDX-License-Identifier: LicenseRef-Kiwi-BSL-1.1
// Copyright (c) 2026 RunKiwi. Licensed under the Business Source License 1.1.
// See ee/LICENSE. This is Control Plane code and is NOT Apache-2.0.

package orchestrator

import (
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/ibreakthecloud/kiwi/ee/auth"
)

// prURLPattern extracts owner/repo/number from a pasted GitHub PR URL —
// the dashboard form's only input, since the underlying createExternalMonitor
// (Task 3) needs those three values, not the URL itself.
var prURLPattern = regexp.MustCompile(`^https://github\.com/([\w.-]+)/([\w.-]+)/pull/(\d+)/?$`)

// handleCreateMonitor serves POST /api/v1/monitors — create a monitor for
// any merged PR (not just a Kiwi-authored one) from a pasted PR URL. This is
// the dashboard-facing counterpart to Phase 1a's webhook-driven creation:
// same resulting row, reached by a human pasting a link instead of a merge
// event arriving.
func (s *Server) handleCreateMonitor(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	claims := auth.ClaimsFromContext(r.Context())
	if claims == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var body struct {
		PRURL string `json:"pr_url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	m := prURLPattern.FindStringSubmatch(body.PRURL)
	if m == nil {
		http.Error(w, "pr_url must look like https://github.com/<owner>/<repo>/pull/<number>", http.StatusBadRequest)
		return
	}
	owner, repo := m[1], m[2]
	number, _ := strconv.Atoi(m[3])

	mon, err := s.createExternalMonitor(r.Context(), claims.OrgID, owner, repo, number, githubAPIDefault)
	if err != nil {
		switch {
		case errors.Is(err, ErrPRNotMerged):
			http.Error(w, "this pull request is not merged yet — create the monitor again after it merges", http.StatusUnprocessableEntity)
		case errors.Is(err, ErrMonitorAlreadyExists):
			http.Error(w, "a monitor already exists for this pull request", http.StatusConflict)
		default:
			http.Error(w, err.Error(), http.StatusBadGateway)
		}
		return
	}

	writeJSON(w, http.StatusCreated, mon)
}

// handleListMonitors serves GET /api/v1/monitors — the org's monitors,
// newest first.
func (s *Server) handleListMonitors(w http.ResponseWriter, r *http.Request) {
	claims := auth.ClaimsFromContext(r.Context())
	if claims == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	monitors, err := s.storage.ListMonitors(r.Context(), claims.OrgID)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"monitors": monitors})
}

// handleCancelMonitor serves POST /api/v1/monitors/{id}/cancel. Registered
// at the "/api/v1/monitors/" prefix (this codebase's routing convention has
// no {id} pattern matching — see handleModels/handleTelemetryMetrics for the
// same TrimPrefix/TrimSuffix idiom), so it parses and validates the suffix
// itself before doing anything else.
func (s *Server) handleCancelMonitor(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	suffix := strings.TrimPrefix(r.URL.Path, "/api/v1/monitors/")
	id := strings.TrimSuffix(suffix, "/cancel")
	if id == "" || id == suffix || strings.Contains(id, "/") {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	claims := auth.ClaimsFromContext(r.Context())
	if claims == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	// Look the monitor up first so a wrong-org id 404s exactly like an
	// unknown one, rather than a 403 revealing that the id exists at all.
	mon, err := s.storage.GetMonitorByID(r.Context(), id)
	if err != nil || mon.OrgID != claims.OrgID {
		http.Error(w, "monitor not found", http.StatusNotFound)
		return
	}

	ok, err := s.storage.CancelMonitor(r.Context(), id)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if !ok {
		http.Error(w, "monitor is not in a cancellable state", http.StatusConflict)
		return
	}
	w.WriteHeader(http.StatusOK)
}
