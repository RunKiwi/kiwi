package telemetry

import (
	"testing"
	"time"
)

func TestIsTelemetryCredentialMatchesRegisteredNames(t *testing.T) {
	if !IsTelemetryCredential("DATADOG_API_KEY") {
		t.Error("DATADOG_API_KEY should be a telemetry credential")
	}
	if !IsTelemetryCredential("PROMETHEUS_BEARER_TOKEN") {
		t.Error("PROMETHEUS_BEARER_TOKEN should be a telemetry credential")
	}
	if IsTelemetryCredential("ANTHROPIC_API_KEY") {
		t.Error("ANTHROPIC_API_KEY must not be reported as a telemetry credential")
	}
}

func TestSpecForUnknownProviderReturnsFalse(t *testing.T) {
	if _, ok := SpecFor("nonexistent"); ok {
		t.Error("SpecFor(\"nonexistent\") returned ok=true")
	}
}

func TestStubProviderReturnsConfiguredResult(t *testing.T) {
	stub := &StubProvider{Result: Result{SampleCount: 42, Mean: 3.14}}
	got, err := stub.Query(nil, "any query", time.Time{}, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if got.SampleCount != 42 || got.Mean != 3.14 {
		t.Errorf("got %+v", got)
	}
}
