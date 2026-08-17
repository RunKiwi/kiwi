package telemetry

import (
	"context"
	"fmt"
	"time"
)

// Result is a two-number summary of a query over a time range — enough for
// the v1 significance check (sample count + mean delta, see the orchestrator
// verdict computation) without carrying a raw series nobody reads yet. A
// standalone-monitor future (Phase 2) may need richer results; this stays
// minimal until that's an actual requirement.
type Result struct {
	SampleCount int     `json:"sample_count"`
	Mean        float64 `json:"mean"`
}

// Provider queries one telemetry backend over an explicit time range. The
// caller (the daemon, see pkg/daemon) decides what range to ask for — a
// Provider does no scheduling or comparison of its own.
type Provider interface {
	Query(ctx context.Context, query string, start, end time.Time) (Result, error)
}

// Spec names one telemetry backend and the credential/config values its
// connector needs out of the daemon's decrypted credential bundle. Mirrors
// pkg/provider's registry pattern (a slice, linear-scanned, not a map or
// switch) deliberately — same shape, different package, no shared code,
// since pkg/provider is LLM-specific (PricingMap, thinking budgets) and none
// of that applies here.
type Spec struct {
	ID        string
	Display   string
	CredNames []string
}

var registry = []Spec{
	{ID: "prometheus", Display: "Prometheus", CredNames: []string{"PROMETHEUS_BASE_URL", "PROMETHEUS_BEARER_TOKEN"}},
	{ID: "datadog", Display: "Datadog", CredNames: []string{"DATADOG_API_KEY", "DATADOG_APP_KEY"}},
}

func Registry() []Spec {
	out := make([]Spec, len(registry))
	copy(out, registry)
	return out
}

func SpecFor(id string) (Spec, bool) {
	for _, s := range registry {
		if s.ID == id {
			return s, true
		}
	}
	return Spec{}, false
}

// IsTelemetryCredential reports whether name is a credential any registered
// telemetry connector needs. Used to exclude these from the sandbox
// test-command environment (Task 6) exactly as provider.IsLLMCredential
// already excludes LLM keys — an org's Datadog/Prometheus secrets must never
// reach model-generated test code.
func IsTelemetryCredential(name string) bool {
	for _, s := range registry {
		for _, c := range s.CredNames {
			if c == name {
				return true
			}
		}
	}
	return false
}

// ProviderFor constructs the connector for id using creds (the daemon's
// already-decrypted credential bundle — see pkg/daemon's cached-credentials
// field, Task 9). Returns an error naming which required credential is
// missing rather than a generic failure, since a misconfigured org is the
// expected failure mode here, not a bug.
func ProviderFor(id string, creds map[string]string) (Provider, error) {
	spec, ok := SpecFor(id)
	if !ok {
		return nil, fmt.Errorf("unknown telemetry provider %q", id)
	}
	for _, name := range spec.CredNames {
		if creds[name] == "" {
			return nil, fmt.Errorf("telemetry provider %q missing credential %q", id, name)
		}
	}
	switch id {
	case "prometheus":
		// NewPrometheusProvider is implemented in Task 4.
		return NewPrometheusProvider(creds["PROMETHEUS_BASE_URL"], creds["PROMETHEUS_BEARER_TOKEN"]), nil
	case "datadog":
		// NewDatadogProvider is implemented in Task 5.
		return NewDatadogProvider(creds["DATADOG_API_KEY"], creds["DATADOG_APP_KEY"]), nil
	default:
		return nil, fmt.Errorf("unknown telemetry provider %q", id)
	}
}
