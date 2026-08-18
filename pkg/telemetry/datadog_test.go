package telemetry

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestDatadogProviderQueriesAndReducesToResult(t *testing.T) {
	var gotAPIKey, gotAppKey string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAPIKey = r.Header.Get("DD-API-KEY")
		gotAppKey = r.Header.Get("DD-APPLICATION-KEY")
		_, _ = w.Write([]byte(`{
			"status": "ok",
			"series": [
				{"pointlist": [[1000000, 10], [1015000, null], [1030000, 30]]}
			]
		}`))
	}))
	defer srv.Close()

	p := &datadogProvider{baseURL: srv.URL, apiKey: "ak", appKey: "appk", client: srv.Client()}
	got, err := p.Query(context.Background(), "avg:trace.checkout{env:prod}", time.Unix(1000, 0), time.Unix(1030, 0))
	if err != nil {
		t.Fatal(err)
	}
	if gotAPIKey != "ak" || gotAppKey != "appk" {
		t.Errorf("headers = %q / %q", gotAPIKey, gotAppKey)
	}
	// The null point must be skipped, not counted as zero.
	if got.SampleCount != 2 {
		t.Errorf("sample count = %d, want 2 (null point skipped)", got.SampleCount)
	}
	if got.Mean != 20 {
		t.Errorf("mean = %v, want 20", got.Mean)
	}
}

func TestDatadogProviderReportsEmptySeries(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"status":"ok","series":[]}`))
	}))
	defer srv.Close()

	p := &datadogProvider{baseURL: srv.URL, apiKey: "ak", appKey: "appk", client: srv.Client()}
	if _, err := p.Query(context.Background(), "q", time.Unix(0, 0), time.Unix(1, 0)); err == nil {
		t.Error("expected an error for an empty series, got nil")
	}
}
