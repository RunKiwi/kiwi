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
