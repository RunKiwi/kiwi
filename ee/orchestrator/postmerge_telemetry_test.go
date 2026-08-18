// SPDX-License-Identifier: LicenseRef-Kiwi-BSL-1.1
// Copyright (c) 2026 RunKiwi. Licensed under the Business Source License 1.1.
// See ee/LICENSE. This is Control Plane code and is NOT Apache-2.0.

package orchestrator

import (
	"testing"

	"github.com/ibreakthecloud/kiwi/pkg/daemon"
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
