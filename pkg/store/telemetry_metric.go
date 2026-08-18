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
// ComparisonLowerIsBetter in the caller if no match), acceptable because a
// metric's query is unique per (org, repo, name) and a poll's query came
// directly from that same row at creation time (Task 11).
func (s *PostgresStore) GetTelemetryMetricByQuery(ctx context.Context, orgID, query string) (*TelemetryMetric, error) {
	var m TelemetryMetric
	if err := s.db.WithContext(ctx).Where("org_id = ? AND query = ?", orgID, query).First(&m).Error; err != nil {
		return nil, err
	}
	return &m, nil
}
