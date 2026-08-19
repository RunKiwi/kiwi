// SPDX-License-Identifier: LicenseRef-Kiwi-BSL-1.1
// Copyright (c) 2026 RunKiwi. Licensed under the Business Source License 1.1.
// See ee/LICENSE. This is Control Plane code and is NOT Apache-2.0.

package orchestrator

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ibreakthecloud/kiwi/pkg/store"
)

// withTelemetryTestLookupHost overrides telemetryTestLookupHost for the
// duration of a test and restores it on cleanup. Tests that need the
// SSRF-destination check to pass against a local httptest.Server (which
// necessarily binds 127.0.0.1, itself a loopback address the real check
// rejects) use this to fake DNS resolution to a public IP — the
// classification logic (isBlockedTelemetryDestIP) itself is never
// overridden, so it still runs for real.
func withTelemetryTestLookupHost(t *testing.T, fn func(host string) ([]string, error)) {
	t.Helper()
	orig := telemetryTestLookupHost
	telemetryTestLookupHost = fn
	t.Cleanup(func() { telemetryTestLookupHost = orig })
}

func TestHandleTelemetryMetricsCreateListDelete(t *testing.T) {
	srv, s := setupWebhookTest(t)
	ctx := context.Background()
	if err := s.DB().Create(&store.Organization{ID: "org1", Name: "acme"}).Error; err != nil {
		t.Fatal(err)
	}

	// Create
	bodyBytes, _ := json.Marshal(map[string]string{
		"repo": "acme/widgets", "name": "checkout_latency", "provider": "datadog",
		"query": "p95:trace.checkout{env:prod}", "comparison_direction": store.ComparisonLowerIsBetter,
	})
	w := httptest.NewRecorder()
	srv.handleTelemetryMetrics(w, authed(http.MethodPost, "/api/v1/telemetry-metrics", string(bodyBytes), "org1"))
	if w.Code != http.StatusCreated {
		t.Fatalf("create: status = %d, body = %s", w.Code, w.Body.String())
	}
	var created store.TelemetryMetric
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.ID == "" || created.Repo != "acme/widgets" {
		t.Errorf("created = %+v", created)
	}

	// List
	w = httptest.NewRecorder()
	srv.handleTelemetryMetrics(w, authed(http.MethodGet, "/api/v1/telemetry-metrics", "", "org1"))
	if w.Code != http.StatusOK {
		t.Fatalf("list: status = %d", w.Code)
	}
	var listRes struct {
		Metrics []store.TelemetryMetric `json:"metrics"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &listRes); err != nil {
		t.Fatal(err)
	}
	if len(listRes.Metrics) != 1 {
		t.Fatalf("got %d metrics, want 1", len(listRes.Metrics))
	}

	// A different org must see none of it.
	w = httptest.NewRecorder()
	srv.handleTelemetryMetrics(w, authed(http.MethodGet, "/api/v1/telemetry-metrics", "", "org2"))
	json.Unmarshal(w.Body.Bytes(), &listRes)
	if len(listRes.Metrics) != 0 {
		t.Fatalf("org2 saw %d of org1's metrics, want 0", len(listRes.Metrics))
	}

	// Delete
	w = httptest.NewRecorder()
	srv.handleTelemetryMetrics(w, authed(http.MethodDelete, "/api/v1/telemetry-metrics/"+created.ID, "", "org1"))
	if w.Code != http.StatusNoContent {
		t.Fatalf("delete: status = %d", w.Code)
	}
	remaining, err := s.ListTelemetryMetricsForOrg(ctx, "org1")
	if err != nil {
		t.Fatal(err)
	}
	if len(remaining) != 0 {
		t.Fatalf("got %d metrics after delete, want 0", len(remaining))
	}
}

func TestHandleTelemetryMetricsCreateRejectsUnknownProvider(t *testing.T) {
	srv, s := setupWebhookTest(t)
	if err := s.DB().Create(&store.Organization{ID: "org1", Name: "acme"}).Error; err != nil {
		t.Fatal(err)
	}
	bodyBytes, _ := json.Marshal(map[string]string{
		"repo": "acme/widgets", "name": "x", "provider": "not-a-real-provider",
		"query": "whatever", "comparison_direction": store.ComparisonLowerIsBetter,
	})
	w := httptest.NewRecorder()
	srv.handleTelemetryMetrics(w, authed(http.MethodPost, "/api/v1/telemetry-metrics", string(bodyBytes), "org1"))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (store-level provider validation should surface as a 400)", w.Code)
	}
}

func TestHandleTestTelemetryQuerySucceedsAgainstConfiguredCredential(t *testing.T) {
	promSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"success","data":{"resultType":"matrix","result":[{"values":[[1,"10"],[2,"20"]]}]}}`))
	}))
	defer promSrv.Close()

	srv, s := setupWebhookTest(t)
	ctx := context.Background()
	if err := s.DB().Create(&store.Organization{ID: "org1", Name: "acme"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.SaveCredential(ctx, "org1", "PROMETHEUS_BASE_URL", store.CredentialTelemetry, promSrv.URL); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveCredential(ctx, "org1", "PROMETHEUS_BEARER_TOKEN", store.CredentialTelemetry, "tok"); err != nil {
		t.Fatal(err)
	}
	// promSrv.URL is a local httptest.Server, which binds 127.0.0.1 — a
	// loopback address the SSRF destination check correctly rejects in
	// production. Fake DNS resolution to a public IP so this test exercises
	// the rest of the flow without weakening the real check (which still
	// dials promSrv.URL for real — only the resolver is faked).
	withTelemetryTestLookupHost(t, func(host string) ([]string, error) {
		return []string{"93.184.216.34"}, nil
	})

	bodyBytes, _ := json.Marshal(map[string]string{"provider": "prometheus", "query": "rate(http_requests_total[5m])"})
	w := httptest.NewRecorder()
	srv.handleTestTelemetryQuery(w, authed(http.MethodPost, "/api/v1/telemetry-metrics/test", string(bodyBytes), "org1"))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	var res struct {
		SampleCount int     `json:"sample_count"`
		Mean        float64 `json:"mean"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	if res.SampleCount != 2 || res.Mean != 15 {
		t.Errorf("got %+v, want SampleCount=2, Mean=15", res)
	}
	// Never leak the credential value into the response.
	if strings.Contains(w.Body.String(), "tok") {
		t.Error("response body contains the raw credential value")
	}
}

func TestHandleTestTelemetryQueryRejectsInternalPrometheusDestination(t *testing.T) {
	srv, s := setupWebhookTest(t)
	ctx := context.Background()
	if err := s.DB().Create(&store.Organization{ID: "org1", Name: "acme"}).Error; err != nil {
		t.Fatal(err)
	}
	// A URL pointing at the cloud metadata address — this must be rejected
	// before prov.Query (and hence any dial) ever runs, regardless of
	// whether anything is listening there.
	if err := s.SaveCredential(ctx, "org1", "PROMETHEUS_BASE_URL", store.CredentialTelemetry, "http://169.254.169.254/"); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveCredential(ctx, "org1", "PROMETHEUS_BEARER_TOKEN", store.CredentialTelemetry, "tok"); err != nil {
		t.Fatal(err)
	}

	bodyBytes, _ := json.Marshal(map[string]string{"provider": "prometheus", "query": "rate(http_requests_total[5m])"})
	w := httptest.NewRecorder()
	srv.handleTestTelemetryQuery(w, authed(http.MethodPost, "/api/v1/telemetry-metrics/test", string(bodyBytes), "org1"))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for an internal-address PROMETHEUS_BASE_URL", w.Code)
	}
	if !strings.Contains(w.Body.String(), "non-routable or internal address") {
		t.Errorf("error body = %q, want the internal-address rejection message", w.Body.String())
	}
}

func TestHandleTestTelemetryQueryFailsClearlyWithNoCredential(t *testing.T) {
	srv, s := setupWebhookTest(t)
	if err := s.DB().Create(&store.Organization{ID: "org1", Name: "acme"}).Error; err != nil {
		t.Fatal(err)
	}
	bodyBytes, _ := json.Marshal(map[string]string{"provider": "datadog", "query": "avg:some.metric{*}"})
	w := httptest.NewRecorder()
	srv.handleTestTelemetryQuery(w, authed(http.MethodPost, "/api/v1/telemetry-metrics/test", string(bodyBytes), "org1"))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for a missing credential", w.Code)
	}
	if !strings.Contains(w.Body.String(), "DATADOG_API_KEY") {
		t.Errorf("error body = %q, want it to name the missing credential", w.Body.String())
	}
}
