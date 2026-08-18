package telemetry

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
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

func TestPrometheusProviderRejectsMultipleSeries(t *testing.T) {
	// Two series is what any non-aggregating query over a multi-label metric
	// returns. Taking the first would silently compare a different series in
	// the baseline call than in the current call.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{
			"status": "success",
			"data": {
				"resultType": "matrix",
				"result": [
					{"metric": {"pod": "a"}, "values": [[1000, "10"]]},
					{"metric": {"pod": "b"}, "values": [[1000, "90"]]}
				]
			}
		}`))
	}))
	defer srv.Close()

	p := NewPrometheusProvider(srv.URL, "t")
	_, err := p.Query(context.Background(), "rate(http_requests_total[5m])", time.Unix(0, 0), time.Unix(60, 0))
	if err == nil {
		t.Fatal("expected an error for a 2-series result, got nil")
	}
	if !strings.Contains(err.Error(), "returned 2 series") {
		t.Errorf("error = %v, want it to name the series count", err)
	}
}

// rangeAwarePrometheus serves a synthetic series whose length is derived from
// the range the caller actually asked for — (end-start)/step + 1 points,
// inclusive of both endpoints, exactly as a real Prometheus range query does.
// This is what makes TestPrometheusSampleCountTracksWindowWidth meaningful: a
// fixed-size fixture would return the same SampleCount for every range and
// the whole assertion would prove nothing.
func rangeAwarePrometheus(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		start, err1 := strconv.ParseInt(q.Get("start"), 10, 64)
		end, err2 := strconv.ParseInt(q.Get("end"), 10, 64)
		step, err3 := strconv.ParseInt(q.Get("step"), 10, 64)
		if err1 != nil || err2 != nil || err3 != nil || step <= 0 || end < start {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		var values []string
		for ts := start; ts <= end; ts += step {
			values = append(values, fmt.Sprintf("[%d,\"100\"]", ts))
		}
		fmt.Fprintf(w, `{"status":"success","data":{"resultType":"matrix","result":[{"metric":{},"values":[%s]}]}}`,
			strings.Join(values, ","))
	}))
}

// TestPrometheusSampleCountTracksWindowWidth is the regression barrier for
// the frozen-comparison-window bug: the poll's CurrentEnd was created ~1
// second after CurrentStart and never advanced, so every query asked for a
// ~1-second range. Prometheus hardcodes step=60, so SampleCount tracks the
// window's width in minutes — a 1-second window can only ever return 1
// sample, permanently below the orchestrator's 30-sample significance floor,
// which made the entire telemetry engine inert.
//
// Both directions are asserted against the same server: a grown window
// clears the floor, and a narrow one stays below it (correctly inconclusive
// rather than silently accepted).
func TestPrometheusSampleCountTracksWindowWidth(t *testing.T) {
	const minSignificantSamples = 30 // mirrors ee/orchestrator's fixed v1 bar

	srv := rangeAwarePrometheus(t)
	defer srv.Close()

	p := NewPrometheusProvider(srv.URL, "t")
	mergedAt := time.Unix(1_700_000_000, 0)

	// A window grown to ~35 minutes past the merge, i.e. what a poll's
	// CurrentEnd looks like after a couple of reschedules.
	wide, err := p.Query(context.Background(), "avg(latency)", mergedAt, mergedAt.Add(35*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("35-minute window: SampleCount = %d", wide.SampleCount)
	if wide.SampleCount < minSignificantSamples {
		t.Errorf("35-minute window SampleCount = %d, want >= %d — a grown window must be able to clear the significance floor",
			wide.SampleCount, minSignificantSamples)
	}

	// The bug's actual failure mode: the window as created, ~1 second wide.
	frozen, err := p.Query(context.Background(), "avg(latency)", mergedAt, mergedAt.Add(1*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("1-second window (the bug's failure mode): SampleCount = %d", frozen.SampleCount)
	if frozen.SampleCount >= minSignificantSamples {
		t.Errorf("1-second window SampleCount = %d, want < %d", frozen.SampleCount, minSignificantSamples)
	}

	// And a rolling 15-minute window — the plausible-looking wrong fix — is
	// still below the floor, which is why the window has to grow from the
	// merge rather than roll.
	narrow, err := p.Query(context.Background(), "avg(latency)", mergedAt, mergedAt.Add(15*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("15-minute rolling window: SampleCount = %d", narrow.SampleCount)
	if narrow.SampleCount >= minSignificantSamples {
		t.Errorf("15-minute window SampleCount = %d, want < %d — a rolling window is not a fix for the frozen-window bug",
			narrow.SampleCount, minSignificantSamples)
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
