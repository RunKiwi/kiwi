// SPDX-License-Identifier: LicenseRef-Kiwi-BSL-1.1
// Copyright (c) 2026 RunKiwi. Licensed under the Business Source License 1.1.
// See ee/LICENSE. This is Control Plane code and is NOT Apache-2.0.

package orchestrator

import (
	"context"
	"encoding/json"
	"log"
	"time"

	"github.com/ibreakthecloud/kiwi/pkg/daemon"
	"github.com/ibreakthecloud/kiwi/pkg/store"
)

const (
	minSignificantSamples  = 30
	regressionRelativeBar  = 0.20 // 20% relative worsening
	pollRescheduleInterval = 15 * time.Minute
	pollStaleClaimAfter    = 10 * time.Minute
)

// evaluateSignificance is Phase 1b's fixed v1 significance bar — not
// per-org configurable, not calibrated against real traffic, a deliberate
// placeholder pending real data (see the design doc's Open Questions). Only
// ever returns store.MonitorStatusRegression as a confident verdict; a
// clean or improved read is never confident enough to finalize VERIFIED —
// that remains Phase 1a's window-elapsed sweep's job alone.
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
		if err := s.storage.RecordPollResult(ctx, poll.ID, time.Now(), string(resultJSON), false); err != nil {
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
func (s *Server) rescheduleOrExpirePoll(ctx context.Context, poll *store.PostMergeTelemetryPoll, resultJSON string) {
	now := time.Now()
	if now.After(poll.WindowEndsAt) {
		if err := s.storage.RecordPollResult(ctx, poll.ID, now, resultJSON, false); err != nil {
			log.Printf("[telemetry] expire poll %s: %v", poll.ID, err)
		}
		return
	}
	next := now.Add(pollRescheduleInterval)
	if err := s.storage.RecordPollResult(ctx, poll.ID, next, resultJSON, true); err != nil {
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
func (s *Server) ReleaseStaleTelemetryPolls(ctx context.Context) {
	n, err := s.storage.ReleaseStalePolls(ctx, time.Now().Add(-pollStaleClaimAfter))
	if err != nil {
		log.Printf("[telemetry] release stale polls: %v", err)
		return
	}
	if n > 0 {
		log.Printf("[telemetry] released %d stale telemetry poll claim(s)", n)
	}
}
