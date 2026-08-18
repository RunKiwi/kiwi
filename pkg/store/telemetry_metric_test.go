package store

import (
	"context"
	"testing"
)

func TestTelemetryMetricRoundTrip(t *testing.T) {
	s := newTestStore(t)
	m := &TelemetryMetric{
		ID:                  "tm_1",
		OrgID:               "org1",
		Repo:                "acme/widgets",
		Name:                "checkout_p95_latency",
		Provider:            "datadog",
		Query:               "p95:trace.checkout{env:prod}",
		ComparisonDirection: ComparisonLowerIsBetter,
	}
	if err := s.CreateTelemetryMetric(context.Background(), m); err != nil {
		t.Fatalf("create: %v", err)
	}

	got, err := s.ListTelemetryMetrics(context.Background(), "org1", "acme/widgets")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(got) != 1 || got[0].Name != "checkout_p95_latency" {
		t.Fatalf("got %+v, want one row named checkout_p95_latency", got)
	}

	// A metric for a different repo must not leak into this list.
	other := &TelemetryMetric{
		ID: "tm_2", OrgID: "org1", Repo: "acme/other-repo", Name: "x",
		Provider: "prometheus", Query: "x", ComparisonDirection: ComparisonHigherIsBetter,
	}
	if err := s.CreateTelemetryMetric(context.Background(), other); err != nil {
		t.Fatalf("create other: %v", err)
	}
	got, err = s.ListTelemetryMetrics(context.Background(), "org1", "acme/widgets")
	if err != nil {
		t.Fatalf("list after second insert: %v", err)
	}
	if len(got) != 1 {
		t.Errorf("list scoped to acme/widgets returned %d rows, want 1", len(got))
	}
}

// TestGetTelemetryMetricByQueryScopesByRepo proves the lookup resolves the
// correct row when two metrics in the same org, across different repos,
// share an identical query string — a real possibility since the DB
// constraint (migrations/0039) is UNIQUE(org_id, repo, name), not
// UNIQUE(org_id, query). An (org, query)-only lookup would nondeterministically
// return whichever row the DB happened to return first, silently applying
// the wrong comparison direction. Scoping by (org, repo, query) must pick
// the row belonging to the requested repo every time.
func TestGetTelemetryMetricByQueryScopesByRepo(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	sharedQuery := "up{job=\"api\"}"
	widgets := &TelemetryMetric{
		ID: "tm_widgets", OrgID: "org1", Repo: "acme/widgets", Name: "uptime",
		Provider: "prometheus", Query: sharedQuery, ComparisonDirection: ComparisonHigherIsBetter,
	}
	gadgets := &TelemetryMetric{
		ID: "tm_gadgets", OrgID: "org1", Repo: "acme/gadgets", Name: "uptime",
		Provider: "prometheus", Query: sharedQuery, ComparisonDirection: ComparisonLowerIsBetter,
	}
	if err := s.CreateTelemetryMetric(ctx, widgets); err != nil {
		t.Fatalf("create widgets metric: %v", err)
	}
	if err := s.CreateTelemetryMetric(ctx, gadgets); err != nil {
		t.Fatalf("create gadgets metric: %v", err)
	}

	got, err := s.GetTelemetryMetricByQuery(ctx, "org1", "acme/widgets", sharedQuery)
	if err != nil {
		t.Fatalf("get for acme/widgets: %v", err)
	}
	if got.ID != "tm_widgets" || got.ComparisonDirection != ComparisonHigherIsBetter {
		t.Errorf("got %+v, want the acme/widgets row (higher_is_better)", got)
	}

	got, err = s.GetTelemetryMetricByQuery(ctx, "org1", "acme/gadgets", sharedQuery)
	if err != nil {
		t.Fatalf("get for acme/gadgets: %v", err)
	}
	if got.ID != "tm_gadgets" || got.ComparisonDirection != ComparisonLowerIsBetter {
		t.Errorf("got %+v, want the acme/gadgets row (lower_is_better)", got)
	}

	if _, err := s.GetTelemetryMetricByQuery(ctx, "org1", "acme/unrelated", sharedQuery); err == nil {
		t.Error("expected no match for a repo with no metric row, got nil error")
	}
}
