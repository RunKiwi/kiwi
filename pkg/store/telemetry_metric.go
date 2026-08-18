package store

import (
	"context"
	"fmt"

	"github.com/ibreakthecloud/kiwi/pkg/telemetry"
)

// CreateTelemetryMetric validates before writing because hand-inserted rows
// are the only provisioning path this table has (there is no dashboard CRUD
// for it yet), and both fields fail silently and invisibly when typo'd. An
// unknown provider is dropped by enqueueTelemetryPolls' SpecFor lookup, so
// the operator sees a metric configured and nothing ever happens; an unknown
// comparison_direction is worse, since the verdict computation only
// special-cases the literal higher-is-better string and anything else falls
// through to lower-is-better semantics — which can invert the verdict. The
// DB CHECK constraint (migrations/0041) is the backstop; this is the error
// message a human actually reads.
func (s *PostgresStore) CreateTelemetryMetric(ctx context.Context, m *TelemetryMetric) error {
	if _, ok := telemetry.SpecFor(m.Provider); !ok {
		return fmt.Errorf("unknown telemetry provider %q", m.Provider)
	}
	if m.ComparisonDirection != ComparisonHigherIsBetter && m.ComparisonDirection != ComparisonLowerIsBetter {
		return fmt.Errorf("invalid comparison_direction %q", m.ComparisonDirection)
	}
	return s.db.WithContext(ctx).Create(m).Error
}

func (s *PostgresStore) ListTelemetryMetrics(ctx context.Context, orgID, repo string) ([]TelemetryMetric, error) {
	var out []TelemetryMetric
	err := s.db.WithContext(ctx).
		Where("org_id = ? AND repo = ?", orgID, repo).
		Order("name").
		Find(&out).Error
	return out, err
}

// GetTelemetryMetricByQuery resolves a poll's comparison direction back to
// its originating metric config. A poll only stores the query string, not
// the metric's ID — this is a best-effort lookup (falls back to
// ComparisonLowerIsBetter in the caller if no match). The lookup is scoped
// by repo, not just org: the DB constraint (migrations/0039) is
// UNIQUE(org_id, repo, name), not UNIQUE(org_id, query) — two metrics in the
// same org across different repos can share an identical query string (e.g.
// both repos exposing the same generic Prometheus query), so an
// (org, query)-only lookup could silently resolve to the wrong repo's
// comparison direction. Scoping by (org, repo, query) matches how the row
// was created: a poll's query came directly from a metric row for that same
// (org, repo) at creation time (Task 11).
func (s *PostgresStore) GetTelemetryMetricByQuery(ctx context.Context, orgID, repo, query string) (*TelemetryMetric, error) {
	var m TelemetryMetric
	if err := s.db.WithContext(ctx).Where("org_id = ? AND repo = ? AND query = ?", orgID, repo, query).First(&m).Error; err != nil {
		return nil, err
	}
	return &m, nil
}

// ListTelemetryMetricsForOrg lists every metric configured for orgID across
// all repos — the dashboard's list view needs this; ListTelemetryMetrics
// (repo-scoped) exists for enqueueTelemetryPolls, which always knows the
// specific repo it's enqueueing for.
func (s *PostgresStore) ListTelemetryMetricsForOrg(ctx context.Context, orgID string) ([]TelemetryMetric, error) {
	var out []TelemetryMetric
	err := s.db.WithContext(ctx).
		Where("org_id = ?", orgID).
		Order("repo, name").
		Find(&out).Error
	return out, err
}

// DeleteTelemetryMetric is org-scoped: a caller cannot delete another org's
// metric, mirroring DeleteModel's pattern exactly. Deleting a metric does
// not affect any poll already in flight — a PostMergeTelemetryPoll copies
// the query text at creation time (see Task 11 of the telemetry-engine
// plan), so an in-progress poll finishes on the query it started with.
func (s *PostgresStore) DeleteTelemetryMetric(ctx context.Context, orgID, id string) error {
	return s.db.WithContext(ctx).Where("org_id = ? AND id = ?", orgID, id).Delete(&TelemetryMetric{}).Error
}
