CREATE TABLE postmerge_telemetry_polls (
    id TEXT PRIMARY KEY,
    org_id TEXT NOT NULL,
    monitor_id TEXT NOT NULL,
    provider TEXT NOT NULL,
    query TEXT NOT NULL,
    baseline_start TIMESTAMPTZ NOT NULL,
    baseline_end TIMESTAMPTZ NOT NULL,
    current_start TIMESTAMPTZ NOT NULL,
    current_end TIMESTAMPTZ NOT NULL,
    next_poll_at TIMESTAMPTZ NOT NULL,
    claimed_at TIMESTAMPTZ,
    window_ends_at TIMESTAMPTZ NOT NULL,
    last_result TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_postmerge_telemetry_polls_due ON postmerge_telemetry_polls (org_id, next_poll_at) WHERE claimed_at IS NULL;
CREATE INDEX idx_postmerge_telemetry_polls_monitor ON postmerge_telemetry_polls (monitor_id);
