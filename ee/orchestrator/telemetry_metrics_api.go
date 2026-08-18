// SPDX-License-Identifier: LicenseRef-Kiwi-BSL-1.1
// Copyright (c) 2026 RunKiwi. Licensed under the Business Source License 1.1.
// See ee/LICENSE. This is Control Plane code and is NOT Apache-2.0.

package orchestrator

import (
	"encoding/json"
	"log"
	"net"
	"net/http"
	"net/url"
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

// telemetryTestLookupHost resolves a hostname to IP addresses for the
// destination-validation check below. It's a var (rather than a direct
// net.LookupHost call) purely so tests can substitute a resolver — the
// existing "live test-query success" test points PROMETHEUS_BASE_URL at a
// local httptest.Server, which necessarily binds 127.0.0.1, so it needs a
// way to satisfy the SSRF check without the check itself trusting loopback
// in production. The classification logic (isBlockedTelemetryDestIP) is
// never overridden by tests — only what IP a hostname resolves to is.
var telemetryTestLookupHost = net.LookupHost

// isBlockedTelemetryDestIP reports whether ip is a destination the
// Control-Plane-side test-query endpoint must refuse to dial. This guards
// only the live "test query" preview (handleTestTelemetryQuery), which runs
// synchronously in the shared multi-tenant Control Plane process — it does
// not apply to pkg/telemetry/prometheus.go itself, where a private-range
// URL (e.g. 10.x.x.x) is the normal, legitimate case for a BYOC daemon
// polling the customer's own in-VPC Prometheus.
func isBlockedTelemetryDestIP(ip net.IP) bool {
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified()
}

// validatePrometheusTestDestination rejects a PROMETHEUS_BASE_URL that
// resolves to a non-routable or internal address before the test-query
// endpoint is allowed to dial it. This validates at request time; it does
// not close a TOCTOU/DNS-rebinding race between validation and the actual
// dial (Go's http.Client re-resolves internally). Closing that fully means
// either a custom Dialer.Control (deliberately not done here — it would
// mean threading a Control-Plane-only flag through NewPrometheusProvider's
// constructor, more invasive than this guard warrants) or, longer-term,
// moving test-query execution to the org's own daemon (already where the
// production telemetry poller runs) instead of the Control Plane.
const errTelemetryTestDestination = "PROMETHEUS_BASE_URL resolves to a non-routable or internal address and cannot be tested from the Control Plane"

func validatePrometheusTestDestination(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return false
	}
	ips, err := telemetryTestLookupHost(u.Hostname())
	if err != nil || len(ips) == 0 {
		return false
	}
	for _, ipStr := range ips {
		ip := net.ParseIP(ipStr)
		if ip == nil || isBlockedTelemetryDestIP(ip) {
			return false
		}
	}
	return true
}

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

	// Prometheus's base URL is org-supplied and this endpoint issues a live
	// synchronous outbound request from the shared multi-tenant Control
	// Plane process — unlike Datadog, whose provider is hardcoded to
	// https://api.datadoghq.com, never an org-supplied destination. Refuse
	// to dial anywhere non-routable/internal (including the cloud metadata
	// address, 169.254.169.254) before ever calling ProviderFor/Query.
	if body.Provider == "prometheus" {
		if !validatePrometheusTestDestination(creds["PROMETHEUS_BASE_URL"]) {
			http.Error(w, errTelemetryTestDestination, http.StatusBadRequest)
			return
		}
	}

	prov, err := telemetry.ProviderFor(body.Provider, creds)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	now := time.Now()
	result, err := prov.Query(r.Context(), body.Query, now.Add(-telemetryTestQueryWindow), now)
	if err != nil {
		// Don't reflect raw upstream response content (prov.Query's error
		// can include up to 4KB of the probed service's response body) back
		// to the browser — log it server-side and return a generic message.
		log.Printf("[telemetry-test] org %s provider %s query failed: %v", claims.OrgID, body.Provider, err)
		http.Error(w, "query failed — check the query syntax and that the metric exists", http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"sample_count": result.SampleCount, "mean": result.Mean})
}
