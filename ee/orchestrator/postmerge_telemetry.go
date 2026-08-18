// SPDX-License-Identifier: LicenseRef-Kiwi-BSL-1.1
// Copyright (c) 2026 RunKiwi. Licensed under the Business Source License 1.1.
// See ee/LICENSE. This is Control Plane code and is NOT Apache-2.0.

package orchestrator

import (
	"context"
	"encoding/json"
	"log"
	"time"

	"github.com/google/uuid"

	"github.com/ibreakthecloud/kiwi/pkg/daemon"
	"github.com/ibreakthecloud/kiwi/pkg/provider"
	"github.com/ibreakthecloud/kiwi/pkg/store"
	"github.com/ibreakthecloud/kiwi/pkg/telemetry"
)

const (
	minSignificantSamples  = 30
	regressionRelativeBar  = 0.20 // 20% relative worsening
	pollRescheduleInterval = 15 * time.Minute
	// pollStaleClaimAfter deliberately sits well above pkg/daemon's
	// telemetryPollBudget (2 minutes): a claim is stamped before the daemon
	// starts querying, so a threshold that merely matches the daemon's worst
	// case can release a claim mid-flight and have the poll re-claimed on the
	// next tick, producing duplicate queries and duplicate reports.
	pollStaleClaimAfter = 5 * time.Minute // headroom above pollTelemetry's 2-minute budget
)

// evaluateSignificance is Phase 1b's fixed v1 significance bar — not
// per-org configurable, not calibrated against real traffic, a deliberate
// placeholder pending real data (see the design doc's Open Questions). Only
// ever returns store.MonitorStatusRegression as a confident verdict; a
// clean or improved read is never confident enough to finalize VERIFIED —
// that remains Phase 1a's window-elapsed sweep's job alone.
//
// The minSignificantSamples floor is implicitly calibrated to Prometheus's
// fixed step=60 range query (pkg/telemetry/prometheus.go), where sample
// count tracks the window's width in minutes — so a poll's growing
// comparison window only clears the floor from roughly 30 minutes
// post-merge onward, and the first poll cycles legitimately read
// inconclusive. Datadog's /api/v1/query picks its own rollup by range and
// may not map to minutes the same way.
func evaluateSignificance(baseline, current daemon.TelemetryResultDTO, direction string) (verdict string, confident bool) {
	if baseline.SampleCount < minSignificantSamples || current.SampleCount < minSignificantSamples {
		return "", false
	}
	if baseline.Mean == 0 {
		return "", false // avoid a divide-by-zero; can't compute a relative change from a zero baseline
	}

	delta := (current.Mean - baseline.Mean) / baseline.Mean
	worse := delta > 0
	magnitude := delta
	if direction == store.ComparisonHigherIsBetter {
		worse = delta < 0
		magnitude = -delta
	}

	if worse && magnitude > regressionRelativeBar {
		return store.MonitorStatusRegression, true
	}
	return "", false
}

// handleTelemetryPollResult is called once per result in a daemon's report
// batch (Task 8). It looks up the poll's parent monitor, evaluates
// significance if the query succeeded, and either finalizes the monitor
// (REGRESSION, reschedule=false — no next poll needed) or persists the
// result and reschedules (reschedule=true) for the next ~15min tick.
func (s *Server) handleTelemetryPollResult(ctx context.Context, orgID string, result daemon.TelemetryPollResult) {
	poll, err := s.storage.GetTelemetryPoll(ctx, result.PollID)
	if err != nil {
		log.Printf("[telemetry] poll %s not found: %v", result.PollID, err)
		return
	}
	if poll.OrgID != orgID {
		log.Printf("[telemetry] poll %s belongs to a different org than the reporting daemon — ignoring", result.PollID)
		return
	}

	resultJSON, _ := json.Marshal(result)

	if result.Error != "" || result.Baseline == nil || result.Current == nil {
		log.Printf("[telemetry] poll %s reported no usable result: %s", result.PollID, result.Error)
		s.rescheduleOrExpirePoll(ctx, poll, string(resultJSON))
		return
	}

	mon, err := s.storage.GetMonitorByID(ctx, poll.MonitorID)
	if err != nil {
		// The monitor is gone (or never existed) but the poll itself is
		// still real — without this, a poll pointing at a missing monitor
		// would get re-claimed and fail here forever (ReleaseStaleTelemetryPolls
		// only clears claimed_at; it never checks WindowEndsAt or advances
		// NextPollAt), so it could never reach its own window-expiry check.
		// Rescheduling/expiring here lets it drain the same way any other
		// unusable result does.
		log.Printf("[telemetry] poll %s: monitor %s not found: %v", result.PollID, poll.MonitorID, err)
		s.rescheduleOrExpirePoll(ctx, poll, string(resultJSON))
		return
	}

	metric, err := s.storage.GetTelemetryMetricByQuery(ctx, orgID, mon.Repo, poll.Query)
	direction := store.ComparisonLowerIsBetter
	if err == nil {
		direction = metric.ComparisonDirection
	}

	verdict, confident := evaluateSignificance(*result.Baseline, *result.Current, direction)
	if confident {
		evidence := "telemetry regression: current mean " + jsonNum(result.Current.Mean) + " vs baseline " + jsonNum(result.Baseline.Mean)
		s.finalizeMonitor(ctx, mon, verdict, evidence)
		if err := s.storage.RecordPollResult(ctx, poll.ID, time.Now(), string(resultJSON), false, time.Now()); err != nil {
			log.Printf("[telemetry] record final result for poll %s: %v", poll.ID, err)
		}
		return
	}

	s.rescheduleOrExpirePoll(ctx, poll, string(resultJSON))
}

// rescheduleOrExpirePoll advances the poll to its next 15-minute tick, or —
// if this poll's own 4h telemetry window has elapsed — stops scheduling it
// (reschedule=false). The parent monitor is untouched either way; Phase 1a's
// unchanged 24h GitHub-native signals and window-elapsed sweep remain the
// backstop that eventually finalizes VERIFIED.
//
// The reschedule branch is also where the poll's comparison window grows:
// CurrentEnd advances to `now` (never to `next`, which is 15 minutes in the
// future — the daemon must never be asked to query a future range), while
// CurrentStart stays anchored at the merge for the poll's whole life. Without
// this advance the window stays frozen at its ~1-second creation width and
// can never reach evaluateSignificance's sample floor, which makes the whole
// telemetry path inert.
func (s *Server) rescheduleOrExpirePoll(ctx context.Context, poll *store.PostMergeTelemetryPoll, resultJSON string) {
	now := time.Now()
	if now.After(poll.WindowEndsAt) {
		if err := s.storage.RecordPollResult(ctx, poll.ID, now, resultJSON, false, now); err != nil {
			log.Printf("[telemetry] expire poll %s: %v", poll.ID, err)
		}
		return
	}
	next := now.Add(pollRescheduleInterval)
	if err := s.storage.RecordPollResult(ctx, poll.ID, next, resultJSON, true, now); err != nil {
		log.Printf("[telemetry] reschedule poll %s: %v", poll.ID, err)
	}
}

func jsonNum(f float64) string {
	b, _ := json.Marshal(f)
	return string(b)
}

// ReleaseStaleTelemetryPolls is the periodic sweep — a poll claimed by a
// due-check but never reported back (daemon crash, lost connectivity)
// would otherwise stay claimed forever, invisible to the next due-check's
// "claimed_at IS NULL" filter. Wired into ee/cmd/kiwid/main.go's existing
// ticker block (Task 12) alongside FinalizePastWindowMonitors.
//
// It carries a second sweep for the mirror-image leak: a poll that was
// never claimed at all (the org's daemon scaled to zero and never came back
// before the 4h window closed) never reaches rescheduleOrExpirePoll's
// expiry check, because that only runs on a report. Both sweeps live under
// this one periodic call so the existing ticker wiring needs no change.
func (s *Server) ReleaseStaleTelemetryPolls(ctx context.Context) {
	n, err := s.storage.ReleaseStalePolls(ctx, time.Now().Add(-pollStaleClaimAfter))
	if err != nil {
		log.Printf("[telemetry] release stale polls: %v", err)
	} else if n > 0 {
		log.Printf("[telemetry] released %d stale telemetry poll claim(s)", n)
	}

	retired, err := s.storage.RetirePastWindowPolls(ctx, time.Now())
	if err != nil {
		log.Printf("[telemetry] retire past-window polls: %v", err)
		return
	}
	if retired > 0 {
		log.Printf("[telemetry] retired %d past-window telemetry poll(s) that were never claimed", retired)
	}
}

const telemetryPollWindow = 4 * time.Hour
const telemetryBaselineLookback = time.Hour

// enqueueTelemetryPolls runs once per monitor, right after Phase 1a's
// createPostMergeMonitor creates it. If the org has no telemetry_metrics
// configured for this repo — the common case until an org opts in — this
// is a no-op and the monitor runs on GitHub-native signals alone, exactly
// as it does today.
//
// A metric is only offered to the selector once every credential name its
// provider requires (telemetry.SpecFor(provider).CredNames) is present in
// the org's saved credentials. The dashboard's "connected" flag is
// per-credential-row (see integrationSpec in dashboard_api.go — a
// "datadog" row and a "datadog-app-key" row are independent), so a saved
// DATADOG_API_KEY alone does not mean the datadog provider is usable; an
// org that saved only one of the two required credentials must not have
// that metric offered as a choice, and this is an expected, common
// mid-setup state, not a bug worth logging.
func (s *Server) enqueueTelemetryPolls(ctx context.Context, mon *store.PostMergeMonitor, intent string) {
	metrics, err := s.storage.ListTelemetryMetrics(ctx, mon.OrgID, mon.Repo)
	if err != nil {
		log.Printf("[telemetry] list metrics for %s: %v", mon.Repo, err)
		return
	}
	if len(metrics) == 0 || s.metricSelector == nil {
		return
	}

	creds, err := s.storage.ListCredentials(ctx, mon.OrgID)
	if err != nil {
		log.Printf("[telemetry] list credentials for monitor %s: %v", mon.ID, err)
		return
	}
	credNames := make(map[string]bool, len(creds))
	for _, c := range creds {
		credNames[c.Name] = true
	}

	// selectable holds only the metrics whose provider is fully configured
	// for this org — options and the later chosen-metric lookup are both
	// built from this, never from the unfiltered metrics slice, so an
	// excluded metric can never be selected even if a selector "chooses" it
	// by name.
	selectable := make([]store.TelemetryMetric, 0, len(metrics))
	for _, m := range metrics {
		spec, ok := telemetry.SpecFor(m.Provider)
		if !ok {
			// CreateTelemetryMetric validates the provider name (and a DB
			// CHECK constraint backstops it), so reaching this branch means a
			// pre-validation row or a genuine bug — either deserves a trace
			// rather than a silent skip that leaves the operator seeing a
			// configured metric that never does anything.
			log.Printf("[telemetry] metric %s has unknown provider %q, skipping", m.Name, m.Provider)
			continue
		}
		configured := true
		for _, name := range spec.CredNames {
			if !credNames[name] {
				configured = false
				break
			}
		}
		if configured {
			selectable = append(selectable, m)
		}
	}
	if len(selectable) == 0 {
		return
	}

	options := make([]provider.MetricOption, 0, len(selectable))
	for _, m := range selectable {
		options = append(options, provider.MetricOption{Name: m.Name})
	}
	// Bounded so a slow or hung provider cannot stall the GitHub webhook
	// response indefinitely — this call runs synchronously inline in
	// createPostMergeMonitor, on the webhook request's own context. A
	// timeout here surfaces as the same "select metric" error path as any
	// other selector failure: logged, no poll, the monitor still runs on
	// GitHub-native signals alone.
	//
	// The timeout derives from context.WithoutCancel(ctx) rather than ctx
	// itself: the 5-second bound is the property worth keeping, while
	// inheriting the webhook request's cancellation means a GitHub-side
	// disconnect kills the selector call instantly and the poll is silently
	// lost. The call stays synchronous and inline — it still completes (or
	// times out) before the handler returns.
	selectCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	chosen, err := s.metricSelector.SelectMetric(selectCtx, intent, options)
	if err != nil {
		log.Printf("[telemetry] select metric for monitor %s: %v", mon.ID, err)
		return
	}
	if chosen == "" {
		return
	}

	var metric *store.TelemetryMetric
	for i := range selectable {
		if selectable[i].Name == chosen {
			metric = &selectable[i]
			break
		}
	}
	if metric == nil {
		log.Printf("[telemetry] selector chose unknown metric %q for monitor %s", chosen, mon.ID)
		return
	}

	now := time.Now()
	poll := &store.PostMergeTelemetryPoll{
		ID:            "poll_" + uuid.New().String(),
		OrgID:         mon.OrgID,
		MonitorID:     mon.ID,
		Provider:      metric.Provider,
		Query:         metric.Query,
		BaselineStart: mon.DeployedAt.Add(-telemetryBaselineLookback),
		BaselineEnd:   mon.DeployedAt,
		CurrentStart:  mon.DeployedAt,
		CurrentEnd:    now,
		NextPollAt:    now,
		WindowEndsAt:  mon.DeployedAt.Add(telemetryPollWindow),
	}
	if err := s.storage.CreateTelemetryPoll(ctx, poll); err != nil {
		log.Printf("[telemetry] create poll for monitor %s: %v", mon.ID, err)
	}
}
