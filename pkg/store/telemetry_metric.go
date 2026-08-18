package store

import "context"

func (s *PostgresStore) CreateTelemetryMetric(ctx context.Context, m *TelemetryMetric) error {
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
