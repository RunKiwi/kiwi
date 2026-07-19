CREATE TABLE IF NOT EXISTS provisioning_requests (
    id TEXT PRIMARY KEY,
    org_id TEXT NOT NULL,
    type TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending',
    created_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_provisioning_requests_org_id ON provisioning_requests(org_id);
