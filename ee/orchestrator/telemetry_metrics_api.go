// SPDX-License-Identifier: LicenseRef-Kiwi-BSL-1.1
// Copyright (c) 2026 RunKiwi. Licensed under the Business Source License 1.1.
// See ee/LICENSE. This is Control Plane code and is NOT Apache-2.0.

package orchestrator

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/ibreakthecloud/kiwi/ee/auth"
	"github.com/ibreakthecloud/kiwi/pkg/store"
	"github.com/ibreakthecloud/kiwi/pkg/telemetry"
)

// handleTelemetryMetrics serves GET/POST /api/v1/telemetry-metrics and
// DELETE /api/v1/telemetry-metrics/{id} — the CRUD surface telemetry_metrics
// never had (Phase 1b's final review flagged this: CreateTelemetryMetric had
// no caller outside pkg/store and its own tests). Mirrors handleModels
// (dashboard_api.go) exactly: same auth pattern, same method switch, same
// path-suffix parsing for the delete case.
func (s *Server) handleTelemetryMetrics(w http.ResponseWriter, r *http.Request) {
	claims := auth.ClaimsFromContext(r.Context())
	if claims == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	if r.Method == http.MethodDelete {
		id := strings.TrimPrefix(r.URL.Path, "/api/v1/telemetry-metrics/")
		if id == "" || strings.Contains(id, "/") {
			http.Error(w, "metric id required", http.StatusBadRequest)
			return
		}
		if err := s.storage.DeleteTelemetryMetric(r.Context(), claims.OrgID, id); err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusNoContent)
		return
	}

	switch r.Method {
	case http.MethodGet:
		metrics, err := s.storage.ListTelemetryMetricsForOrg(r.Context(), claims.OrgID)
		if err != nil {
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{"metrics": metrics})
	case http.MethodPost:
		var body struct {
			Repo                string `json:"repo"`
			Name                string `json:"name"`
			Provider            string `json:"provider"`
			Query               string `json:"query"`
			ComparisonDirection string `json:"comparison_direction"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid JSON body", http.StatusBadRequest)
			return
		}
		if strings.TrimSpace(body.Repo) == "" || strings.TrimSpace(body.Name) == "" || strings.TrimSpace(body.Query) == "" {
			http.Error(w, "repo, name, and query are required", http.StatusBadRequest)
			return
		}
		m := &store.TelemetryMetric{
			ID:                  "tm_" + uuid.New().String(),
			OrgID:               claims.OrgID,
			Repo:                body.Repo,
			Name:                body.Name,
			Provider:            body.Provider,
			Query:               body.Query,
			ComparisonDirection: body.ComparisonDirection,
		}
		// CreateTelemetryMetric validates Provider and ComparisonDirection
		// (Phase 1b's final review fix) — surface that as a 400, not a 500.
		if err := s.storage.CreateTelemetryMetric(r.Context(), m); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, http.StatusCreated, m)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// telemetryTestQueryWindow is how far back the live test-query endpoint
// looks — short and fixed, since this is a "does this query mechanically
// work" preview, not a real baseline/current comparison.
const telemetryTestQueryWindow = 15 * time.Minute

// handleTestTelemetryQuery serves POST /api/v1/telemetry-metrics/test — runs
// a candidate query live against the org's already-saved credentials and
// returns the result, without persisting anything. This is what lets the
// dashboard form catch a broken or mistyped query before it's saved instead
// of it silently producing no verdict (or a wrong one) weeks later.
func (s *Server) handleTestTelemetryQuery(w http.ResponseWriter, r *http.Request) {
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
		Provider string `json:"provider"`
		Query    string `json:"query"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid JSON body", http.StatusBadRequest)
		return
	}

	spec, ok := telemetry.SpecFor(body.Provider)
	if !ok {
		http.Error(w, "unknown telemetry provider", http.StatusBadRequest)
		return
	}

	// Build only the credential map this one provider needs — never the
	// org's whole bundle — and never let a value reach the response.
	creds := make(map[string]string, len(spec.CredNames))
	for _, name := range spec.CredNames {
		val, err := s.storage.GetCredentialPlaintext(r.Context(), claims.OrgID, name)
		if err != nil || val == "" {
			http.Error(w, "missing credential "+name+" — configure it under Integrations first", http.StatusBadRequest)
			return
		}
		creds[name] = val
	}

	prov, err := telemetry.ProviderFor(body.Provider, creds)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	now := time.Now()
	result, err := prov.Query(r.Context(), body.Query, now.Add(-telemetryTestQueryWindow), now)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"sample_count": result.SampleCount, "mean": result.Mean})
}
