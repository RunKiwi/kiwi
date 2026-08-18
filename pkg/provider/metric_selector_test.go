package provider

import "testing"

// Compile-time checks that the live providers satisfy MetricSelector,
// mirroring tokenreporter_test.go's assertion for AnthropicProvider.
var (
	_ MetricSelector = (*AnthropicProvider)(nil)
	_ MetricSelector = (*GeminiProvider)(nil)
	_ MetricSelector = (*OpenAIProvider)(nil)
	_ MetricSelector = (*MockMetricSelector)(nil)
)

func TestParseMetricSelectionValidJSON(t *testing.T) {
	sel, err := parseMetricSelection(`{"metric_name": "checkout_p95_latency", "reason": "task is about checkout speed"}`)
	if err != nil {
		t.Fatal(err)
	}
	if sel.MetricName != "checkout_p95_latency" {
		t.Errorf("got %+v", sel)
	}
}

func TestParseMetricSelectionNoneChosen(t *testing.T) {
	sel, err := parseMetricSelection(`{"metric_name": "", "reason": "no configured metric is relevant to this task"}`)
	if err != nil {
		t.Fatal(err)
	}
	if sel.MetricName != "" {
		t.Errorf("got %+v, want empty metric_name to mean \"none chosen\"", sel)
	}
}

func TestMockMetricSelectorReturnsConfiguredChoice(t *testing.T) {
	m := &MockMetricSelector{Choice: "checkout_p95_latency"}
	got, err := m.SelectMetric(nil, "speed up checkout", []MetricOption{{Name: "checkout_p95_latency"}})
	if err != nil {
		t.Fatal(err)
	}
	if got != "checkout_p95_latency" {
		t.Errorf("got %q", got)
	}
}
