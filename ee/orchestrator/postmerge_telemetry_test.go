// SPDX-License-Identifier: LicenseRef-Kiwi-BSL-1.1
// Copyright (c) 2026 RunKiwi. Licensed under the Business Source License 1.1.
// See ee/LICENSE. This is Control Plane code and is NOT Apache-2.0.

package orchestrator

import (
	"context"
	"testing"
	"time"

	"github.com/ibreakthecloud/kiwi/pkg/daemon"
	"github.com/ibreakthecloud/kiwi/pkg/provider"
	"github.com/ibreakthecloud/kiwi/pkg/store"
)

func TestEvaluateSignificanceRequiresMinimumSamples(t *testing.T) {
	baseline := daemon.TelemetryResultDTO{SampleCount: 10, Mean: 100}
	current := daemon.TelemetryResultDTO{SampleCount: 10, Mean: 200}
	_, confident := evaluateSignificance(baseline, current, store.ComparisonLowerIsBetter)
	if confident {
		t.Error("confident = true with only 10 samples, want false (below the 30-sample floor)")
	}
}

func TestEvaluateSignificanceDetectsRegressionLowerIsBetter(t *testing.T) {
	baseline := daemon.TelemetryResultDTO{SampleCount: 40, Mean: 100}
	current := daemon.TelemetryResultDTO{SampleCount: 40, Mean: 125} // 25% worse, latency-style metric
	verdict, confident := evaluateSignificance(baseline, current, store.ComparisonLowerIsBetter)
	if !confident || verdict != store.MonitorStatusRegression {
		t.Errorf("verdict = %q, confident = %v, want REGRESSION/true", verdict, confident)
	}
}

func TestEvaluateSignificanceDetectsRegressionHigherIsBetter(t *testing.T) {
	baseline := daemon.TelemetryResultDTO{SampleCount: 40, Mean: 1000}
	current := daemon.TelemetryResultDTO{SampleCount: 40, Mean: 750} // 25% worse, throughput-style metric
	verdict, confident := evaluateSignificance(baseline, current, store.ComparisonHigherIsBetter)
	if !confident || verdict != store.MonitorStatusRegression {
		t.Errorf("verdict = %q, confident = %v, want REGRESSION/true", verdict, confident)
	}
}

func TestEvaluateSignificanceStaysInconclusiveBelowThreshold(t *testing.T) {
	baseline := daemon.TelemetryResultDTO{SampleCount: 40, Mean: 100}
	current := daemon.TelemetryResultDTO{SampleCount: 40, Mean: 105} // 5% worse, below the 20% bar
	_, confident := evaluateSignificance(baseline, current, store.ComparisonLowerIsBetter)
	if confident {
		t.Error("confident = true at a 5%% delta, want false — must not finalize on noise")
	}
}

func TestEvaluateSignificanceNeverFinalizesVerified(t *testing.T) {
	// An improvement (or a clean read) must never itself finalize the
	// monitor — only Phase 1a's window-elapsed sweep calls VERIFIED. A
	// single metric reading "better than baseline" is not proof of no
	// regression elsewhere.
	baseline := daemon.TelemetryResultDTO{SampleCount: 40, Mean: 100}
	current := daemon.TelemetryResultDTO{SampleCount: 40, Mean: 50} // much better
	verdict, confident := evaluateSignificance(baseline, current, store.ComparisonLowerIsBetter)
	if confident || verdict == store.MonitorStatusVerified {
		t.Errorf("verdict = %q, confident = %v, want never VERIFIED from this function", verdict, confident)
	}
}

func TestEnqueueTelemetryPollsCreatesAPollWhenAMetricIsConfigured(t *testing.T) {
	srv, s := setupWebhookTest(t) // reuse Phase 1a's existing helper
	orgID := "org1"
	if err := s.DB().Create(&store.Organization{ID: orgID, Name: "acme"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.CreateTelemetryMetric(context.Background(), &store.TelemetryMetric{
		ID: "tm_1", OrgID: orgID, Repo: "acme/widgets", Name: "checkout_p95_latency",
		Provider: "datadog", Query: "p95:trace.checkout{env:prod}", ComparisonDirection: store.ComparisonLowerIsBetter,
	}); err != nil {
		t.Fatal(err)
	}
	// Both credentials the datadog provider requires must be present for the
	// metric to be offered as an option (see the credential-completeness test
	// below) — save both here so this test isolates metric selection itself.
	if err := s.SaveCredential(context.Background(), orgID, "DATADOG_API_KEY", store.CredentialTelemetry, "dd-api-key"); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveCredential(context.Background(), orgID, "DATADOG_APP_KEY", store.CredentialTelemetry, "dd-app-key"); err != nil {
		t.Fatal(err)
	}
	srv.metricSelector = &provider.MockMetricSelector{Choice: "checkout_p95_latency"}

	mon := &store.PostMergeMonitor{
		ID: "mon_1", OrgID: orgID, JobID: "job1", Repo: "acme/widgets", PRNumber: 42,
		MergeCommitSHA: "abc", Status: store.MonitorStatusMonitoring,
		DeployedAt: time.Now(), WindowEndsAt: time.Now().Add(24 * time.Hour),
	}

	srv.enqueueTelemetryPolls(context.Background(), mon, "speed up checkout")

	var polls []store.PostMergeTelemetryPoll
	if err := s.DB().Where("monitor_id = ?", mon.ID).Find(&polls).Error; err != nil {
		t.Fatal(err)
	}
	if len(polls) != 1 {
		t.Fatalf("got %d polls, want 1", len(polls))
	}
	if polls[0].Query != "p95:trace.checkout{env:prod}" {
		t.Errorf("query = %q", polls[0].Query)
	}
}

func TestEnqueueTelemetryPollsSkipsWhenNoMetricSelected(t *testing.T) {
	srv, s := setupWebhookTest(t)
	orgID := "org1"
	if err := s.DB().Create(&store.Organization{ID: orgID, Name: "acme"}).Error; err != nil {
		t.Fatal(err)
	}
	srv.metricSelector = &provider.MockMetricSelector{Choice: ""} // nothing configured is relevant

	mon := &store.PostMergeMonitor{ID: "mon_1", OrgID: orgID, JobID: "job1", Repo: "acme/widgets", MergeCommitSHA: "abc", DeployedAt: time.Now()}
	srv.enqueueTelemetryPolls(context.Background(), mon, "unrelated task")

	var polls []store.PostMergeTelemetryPoll
	if err := s.DB().Where("monitor_id = ?", mon.ID).Find(&polls).Error; err != nil {
		t.Fatal(err)
	}
	if len(polls) != 0 {
		t.Errorf("got %d polls, want 0", len(polls))
	}
}

// TestEnqueueTelemetryPollsSkipsMetricWithIncompleteCredentials proves the
// forward note from Task 6's review: a dashboard "connected" flag is
// per-credential-row, not per-provider, so a org can have saved
// DATADOG_API_KEY without DATADOG_APP_KEY and still show as "having a
// datadog credential." enqueueTelemetryPolls must check
// telemetry.SpecFor(provider).CredNames in full before ever offering a
// metric as an option — not rely on any single row's existence.
//
// A second, fully-configured prometheus metric is seeded alongside the
// incomplete datadog one so `selectable` is non-empty and the selector is
// genuinely consulted — otherwise enqueueTelemetryPolls would return at its
// `len(selectable) == 0` early-out before ever reaching the chosen-metric
// resolution loop, and the test would prove nothing about that loop scanning
// `selectable` rather than the unfiltered `metrics`. The MockMetricSelector
// is configured to "choose" the excluded datadog metric's exact name (not
// the prometheus one it was actually offered) — proving the zero-poll
// result comes from the resolution loop failing to find that name in
// `selectable`, not from the selector declining or from nothing being
// offered at all.
func TestEnqueueTelemetryPollsSkipsMetricWithIncompleteCredentials(t *testing.T) {
	srv, s := setupWebhookTest(t)
	ctx := context.Background()
	orgID := "org1"
	if err := s.DB().Create(&store.Organization{ID: orgID, Name: "acme"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := s.CreateTelemetryMetric(ctx, &store.TelemetryMetric{
		ID: "tm_1", OrgID: orgID, Repo: "acme/widgets", Name: "checkout_p95_latency",
		Provider: "datadog", Query: "p95:trace.checkout{env:prod}", ComparisonDirection: store.ComparisonLowerIsBetter,
	}); err != nil {
		t.Fatal(err)
	}
	// Only one of datadog's two required credentials is saved.
	if err := s.SaveCredential(ctx, orgID, "DATADOG_API_KEY", store.CredentialTelemetry, "dd-api-key"); err != nil {
		t.Fatal(err)
	}
	// A second, fully-configured metric on a different provider, so
	// `selectable` is non-empty and SelectMetric is actually called.
	if err := s.CreateTelemetryMetric(ctx, &store.TelemetryMetric{
		ID: "tm_2", OrgID: orgID, Repo: "acme/widgets", Name: "request_rate",
		Provider: "prometheus", Query: "rate(http_requests_total[5m])", ComparisonDirection: store.ComparisonHigherIsBetter,
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveCredential(ctx, orgID, "PROMETHEUS_BASE_URL", store.CredentialTelemetry, "https://prom.acme.internal"); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveCredential(ctx, orgID, "PROMETHEUS_BEARER_TOKEN", store.CredentialTelemetry, "prom-token"); err != nil {
		t.Fatal(err)
	}

	// A selector that would happily "choose" the excluded datadog metric by
	// name even though only the prometheus metric was actually offered —
	// proving the zero-poll result comes from the resolution loop's scan of
	// `selectable`, not from the selector declining.
	srv.metricSelector = &provider.MockMetricSelector{Choice: "checkout_p95_latency"}

	mon := &store.PostMergeMonitor{
		ID: "mon_1", OrgID: orgID, JobID: "job1", Repo: "acme/widgets", PRNumber: 42,
		MergeCommitSHA: "abc", Status: store.MonitorStatusMonitoring,
		DeployedAt: time.Now(), WindowEndsAt: time.Now().Add(24 * time.Hour),
	}

	srv.enqueueTelemetryPolls(ctx, mon, "speed up checkout")

	var polls []store.PostMergeTelemetryPoll
	if err := s.DB().Where("monitor_id = ?", mon.ID).Find(&polls).Error; err != nil {
		t.Fatal(err)
	}
	if len(polls) != 0 {
		t.Fatalf("got %d polls, want 0 — a metric with an incomplete credential set must never be offered", len(polls))
	}
}
