CREATE TABLE telemetry_metrics (
    id TEXT PRIMARY KEY,
    org_id TEXT NOT NULL,
    repo TEXT NOT NULL,
    name TEXT NOT NULL,
    provider TEXT NOT NULL,
    query TEXT NOT NULL,
    comparison_direction TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (org_id, repo, name)
);

CREATE INDEX idx_telemetry_metrics_org_repo ON telemetry_metrics (org_id, repo);
