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
