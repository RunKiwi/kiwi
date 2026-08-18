// SPDX-License-Identifier: LicenseRef-Kiwi-BSL-1.1
// Copyright (c) 2026 RunKiwi. Licensed under the Business Source License 1.1.
// See ee/LICENSE. This is Control Plane code and is NOT Apache-2.0.

package orchestrator

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ibreakthecloud/kiwi/pkg/daemon"
	"github.com/ibreakthecloud/kiwi/pkg/provider"
	"github.com/ibreakthecloud/kiwi/pkg/store"
	"github.com/ibreakthecloud/kiwi/pkg/telemetry"
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

// seedMonitorAndPoll creates a monitor plus one telemetry poll whose
// comparison window is exactly as enqueueTelemetryPolls creates it: anchored
// at the merge, and ~1 second wide. WindowEndsAt is deliberately in the
// future so the reschedule branch is exercised, not the expiry branch.
func seedMonitorAndPoll(t *testing.T, s *store.PostgresStore, mergedAt time.Time) *store.PostMergeTelemetryPoll {
	t.Helper()
	ctx := context.Background()
	mon := &store.PostMergeMonitor{
		ID: "mon_win", OrgID: "org1", JobID: "job1", Repo: "acme/widgets", PRNumber: 7,
		MergeCommitSHA: "abc", Status: store.MonitorStatusMonitoring,
		DeployedAt: mergedAt, WindowEndsAt: time.Now().Add(24 * time.Hour),
	}
	if err := s.CreateMonitor(ctx, mon); err != nil {
		t.Fatalf("create monitor: %v", err)
	}
	poll := &store.PostMergeTelemetryPoll{
		ID: "poll_win", OrgID: "org1", MonitorID: mon.ID, Provider: "prometheus", Query: "avg(latency)",
		BaselineStart: mergedAt.Add(-time.Hour), BaselineEnd: mergedAt,
		CurrentStart: mergedAt,
		CurrentEnd:   mergedAt.Add(time.Second),
		NextPollAt:   time.Now(),
		WindowEndsAt: time.Now().Add(4 * time.Hour),
	}
	if err := s.CreateTelemetryPoll(ctx, poll); err != nil {
		t.Fatalf("create poll: %v", err)
	}
	return poll
}

// TestReschedulingAdvancesTheComparisonWindow is the regression barrier for
// the defect that made this whole feature inert: a poll's CurrentEnd was
// written once at creation, ~1 second after CurrentStart, and never advanced.
// Every one of the up-to-16 polls over 4 hours re-queried that same ~1-second
// range, so the sample count could never reach the significance floor and no
// REGRESSION verdict was reachable. Nothing caught it because no test drove a
// poll through a full cycle and inspected what changed.
func TestReschedulingAdvancesTheComparisonWindow(t *testing.T) {
	srv, s := setupWebhookTest(t)
	ctx := context.Background()
	if err := s.DB().Create(&store.Organization{ID: "org1", Name: "acme"}).Error; err != nil {
		t.Fatal(err)
	}
	mergedAt := time.Now().Add(-35 * time.Minute)
	poll := seedMonitorAndPoll(t, s, mergedAt)

	// A below-floor, non-regression result — the common case in the first
	// cycles, and the one that takes the reschedule branch.
	srv.handleTelemetryPollResult(ctx, "org1", daemon.TelemetryPollResult{
		PollID:   poll.ID,
		Baseline: &daemon.TelemetryResultDTO{SampleCount: 1, Mean: 100},
		Current:  &daemon.TelemetryResultDTO{SampleCount: 1, Mean: 101},
	})

	got, err := s.GetTelemetryPoll(ctx, poll.ID)
	if err != nil {
		t.Fatalf("re-fetch poll: %v", err)
	}

	// Pin which branch ran: a rescheduled poll's next tick is ~15 minutes out,
	// not the ~365-day terminal sentinel a finalized/expired poll gets.
	if got.NextPollAt.After(time.Now().Add(24 * time.Hour)) {
		t.Fatalf("NextPollAt = %v — this took the terminal branch, so it never exercised rescheduling", got.NextPollAt)
	}
	if !got.NextPollAt.After(time.Now().Add(10 * time.Minute)) {
		t.Errorf("NextPollAt = %v, want roughly 15 minutes out", got.NextPollAt)
	}

	if !got.CurrentEnd.After(poll.CurrentEnd) {
		t.Errorf("CurrentEnd = %v, want later than the poll's original %v — the comparison window must grow on every reschedule",
			got.CurrentEnd, poll.CurrentEnd)
	}
	if width := got.CurrentEnd.Sub(got.CurrentStart); width < 30*time.Minute {
		t.Errorf("grown window is %v wide, want >= 30m given a merge 35 minutes ago", width)
	}
	if drift := got.CurrentStart.Sub(poll.CurrentStart); drift > time.Second || drift < -time.Second {
		t.Errorf("CurrentStart = %v, want unchanged at %v — the window's start stays anchored at the merge",
			got.CurrentStart, poll.CurrentStart)
	}
}

// TestGrownWindowCrossesSignificanceFloorEndToEnd composes the two halves of
// the fix against a real Prometheus backend: it queries the poll's stored
// window before and after a reschedule and runs the real significance check
// on both. The frozen window is structurally incapable of a verdict; the
// grown one produces the REGRESSION the operator configured the metric for.
// Same server, same fixture, same query — only the window differs.
func TestGrownWindowCrossesSignificanceFloorEndToEnd(t *testing.T) {
	srv, s := setupWebhookTest(t)
	ctx := context.Background()
	if err := s.DB().Create(&store.Organization{ID: "org1", Name: "acme"}).Error; err != nil {
		t.Fatal(err)
	}
	mergedAt := time.Now().Add(-35 * time.Minute)

	// A Prometheus that serves one point per step over whatever range is
	// asked for (so sample count tracks window width, as the real one does at
	// step=60), reading 25% worse after the merge than before it.
	prom := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		start, _ := strconv.ParseInt(q.Get("start"), 10, 64)
		end, _ := strconv.ParseInt(q.Get("end"), 10, 64)
		step, _ := strconv.ParseInt(q.Get("step"), 10, 64)
		if step <= 0 || end < start {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		value := "100" // pre-merge baseline
		if start >= mergedAt.Unix() {
			value = "125" // post-merge: 25% worse, above the 20% bar
		}
		points := make([]string, 0)
		for ts := start; ts <= end; ts += step {
			points = append(points, fmt.Sprintf("[%d,%q]", ts, value))
		}
		fmt.Fprintf(w, `{"status":"success","data":{"resultType":"matrix","result":[{"metric":{},"values":[%s]}]}}`,
			strings.Join(points, ","))
	}))
	defer prom.Close()

	prov, err := telemetry.ProviderFor("prometheus", map[string]string{
		"PROMETHEUS_BASE_URL":     prom.URL,
		"PROMETHEUS_BEARER_TOKEN": "t",
	})
	if err != nil {
		t.Fatal(err)
	}

	poll := seedMonitorAndPoll(t, s, mergedAt)

	// What the daemon would have queried before the fix: the frozen,
	// ~1-second-wide window the poll was created with.
	frozenBaseline, err := prov.Query(ctx, poll.Query, poll.BaselineStart, poll.BaselineEnd)
	if err != nil {
		t.Fatal(err)
	}
	frozenCurrent, err := prov.Query(ctx, poll.Query, poll.CurrentStart, poll.CurrentEnd)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("frozen window: baseline SampleCount = %d, current SampleCount = %d",
		frozenBaseline.SampleCount, frozenCurrent.SampleCount)
	if _, confident := evaluateSignificance(
		daemon.TelemetryResultDTO{SampleCount: frozenBaseline.SampleCount, Mean: frozenBaseline.Mean},
		daemon.TelemetryResultDTO{SampleCount: frozenCurrent.SampleCount, Mean: frozenCurrent.Mean},
		store.ComparisonLowerIsBetter,
	); confident {
		t.Errorf("frozen ~1s window produced a confident verdict at SampleCount = %d — the test fixture is not range-aware",
			frozenCurrent.SampleCount)
	}

	// Now drive one reschedule, which is what grows the window.
	srv.handleTelemetryPollResult(ctx, "org1", daemon.TelemetryPollResult{
		PollID:   poll.ID,
		Baseline: &daemon.TelemetryResultDTO{SampleCount: frozenBaseline.SampleCount, Mean: frozenBaseline.Mean},
		Current:  &daemon.TelemetryResultDTO{SampleCount: frozenCurrent.SampleCount, Mean: frozenCurrent.Mean},
	})
	grown, err := s.GetTelemetryPoll(ctx, poll.ID)
	if err != nil {
		t.Fatal(err)
	}

	grownBaseline, err := prov.Query(ctx, grown.Query, grown.BaselineStart, grown.BaselineEnd)
	if err != nil {
		t.Fatal(err)
	}
	grownCurrent, err := prov.Query(ctx, grown.Query, grown.CurrentStart, grown.CurrentEnd)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("grown window: baseline SampleCount = %d, current SampleCount = %d (means %v vs %v)",
		grownBaseline.SampleCount, grownCurrent.SampleCount, grownBaseline.Mean, grownCurrent.Mean)
	if grownCurrent.SampleCount < minSignificantSamples {
		t.Fatalf("grown window current SampleCount = %d, want >= %d", grownCurrent.SampleCount, minSignificantSamples)
	}

	verdict, confident := evaluateSignificance(
		daemon.TelemetryResultDTO{SampleCount: grownBaseline.SampleCount, Mean: grownBaseline.Mean},
		daemon.TelemetryResultDTO{SampleCount: grownCurrent.SampleCount, Mean: grownCurrent.Mean},
		store.ComparisonLowerIsBetter,
	)
	if !confident || verdict != store.MonitorStatusRegression {
		t.Errorf("grown window verdict = %q, confident = %v, want REGRESSION/true", verdict, confident)
	}
}

// TestReleaseStaleTelemetryPollsAlsoRetiresPastWindowPolls pins the second
// sweep to the same periodic call, so the ticker wiring in ee/cmd/kiwid
// keeps covering both leaks.
func TestReleaseStaleTelemetryPollsAlsoRetiresPastWindowPolls(t *testing.T) {
	srv, s := setupWebhookTest(t)
	ctx := context.Background()

	poll := &store.PostMergeTelemetryPoll{
		ID: "poll_abandoned", OrgID: "org1", MonitorID: "mon_1", Provider: "prometheus", Query: "up",
		BaselineStart: time.Now().Add(-5 * time.Hour), BaselineEnd: time.Now().Add(-4 * time.Hour),
		CurrentStart: time.Now().Add(-4 * time.Hour),
		CurrentEnd:   time.Now().Add(-4 * time.Hour).Add(time.Second),
		// Due for hours, never claimed (the org's daemon was scaled to zero),
		// and its 4h window has since closed.
		NextPollAt:   time.Now().Add(-4 * time.Hour),
		WindowEndsAt: time.Now().Add(-1 * time.Minute),
	}
	if err := s.CreateTelemetryPoll(ctx, poll); err != nil {
		t.Fatal(err)
	}

	srv.ReleaseStaleTelemetryPolls(ctx)

	claimed, err := s.ClaimDuePolls(ctx, "org1", time.Now(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(claimed) != 0 {
		t.Errorf("claimed %d polls after the sweep, want 0 — a past-window, never-claimed poll must be retired, not handed to a daemon that finally comes back online", len(claimed))
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
