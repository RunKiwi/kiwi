package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// MetricOption is one candidate an org has configured for a repo — the
// selector picks among these, it never invents a query of its own (Phase
// 1a's design doc: "operator-configured, task's intent picks among them,"
// not schema discovery).
type MetricOption struct {
	Name string
	// Description is optional operator-supplied context on what the metric
	// means, if the dashboard form Task B (monitor management plan) adds
	// one — empty is fine, the name alone is often enough signal.
	Description string
}

// MetricSelector picks at most one metric option relevant to a task's
// stated intent, or none if nothing configured is relevant. Mirrors
// Critic.ReviewEdit's shape (bounded context, respond-only-JSON, defensive
// parsing) rather than the full agentic Completer.Complete path — this is a
// short-list classification, not task decomposition.
type MetricSelector interface {
	SelectMetric(ctx context.Context, intent string, options []MetricOption) (metricName string, err error)
}

type metricSelection struct {
	MetricName string `json:"metric_name"`
	Reason     string `json:"reason"`
}

// extractJSONObject pulls a JSON object out of a possibly fenced model
// response. pkg/provider cannot reuse ee/planner's equivalent helper — ee/
// is BSL-licensed and pkg/provider is Apache-2.0, and
// pkg/licensing_boundary_test.go enforces that an Apache-2.0 package never
// imports ee/ — so this is a small, deliberate duplication rather than a
// shared helper.
func extractJSONObject(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.Index(s, "```"); i >= 0 {
		s = s[i+3:]
		s = strings.TrimPrefix(s, "json")
		if j := strings.Index(s, "```"); j >= 0 {
			s = s[:j]
		}
	}
	start := strings.Index(s, "{")
	end := strings.LastIndex(s, "}")
	if start >= 0 && end > start {
		return s[start : end+1]
	}
	return s
}

func parseMetricSelection(raw string) (metricSelection, error) {
	var sel metricSelection
	if err := json.Unmarshal([]byte(extractJSONObject(raw)), &sel); err != nil {
		return metricSelection{}, fmt.Errorf("parse metric selection: %w", err)
	}
	return sel, nil
}

// MockMetricSelector is the test double — always returns Choice regardless
// of intent/options, matching the existing MockCritic pattern.
type MockMetricSelector struct {
	Choice string
	Err    error
}

func (m *MockMetricSelector) SelectMetric(ctx context.Context, intent string, options []MetricOption) (string, error) {
	if m.Err != nil {
		return "", m.Err
	}
	return m.Choice, nil
}
