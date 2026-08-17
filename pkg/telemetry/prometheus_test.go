package telemetry

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestPrometheusProviderQueriesRangeAndReducesToResult(t *testing.T) {
	var gotPath, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`{
			"status": "success",
			"data": {
				"resultType": "matrix",
				"result": [
					{"metric": {}, "values": [[1000, "10"], [1015, "20"], [1030, "30"]]}
				]
			}
		}`))
	}))
	defer srv.Close()

	p := NewPrometheusProvider(srv.URL, "test-token")
	start := time.Unix(1000, 0)
	end := time.Unix(1030, 0)
	got, err := p.Query(context.Background(), "up", start, end)
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/v1/query_range" {
		t.Errorf("path = %s", gotPath)
	}
	if gotAuth != "Bearer test-token" {
		t.Errorf("authorization = %q", gotAuth)
	}
	if got.SampleCount != 3 {
		t.Errorf("sample count = %d, want 3", got.SampleCount)
	}
	if got.Mean != 20 {
		t.Errorf("mean = %v, want 20", got.Mean)
	}
}

func TestPrometheusProviderReportsEmptyResult(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"status":"success","data":{"resultType":"matrix","result":[]}}`))
	}))
	defer srv.Close()

	p := NewPrometheusProvider(srv.URL, "t")
	if _, err := p.Query(context.Background(), "up", time.Unix(0, 0), time.Unix(1, 0)); err == nil {
		t.Error("expected an error for an empty result set, got nil")
	}
}

func TestPrometheusProviderReportsAPIErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	p := NewPrometheusProvider(srv.URL, "t")
	if _, err := p.Query(context.Background(), "up", time.Unix(0, 0), time.Unix(1, 0)); err == nil {
		t.Error("expected an error for a 500, got nil")
	}
}
