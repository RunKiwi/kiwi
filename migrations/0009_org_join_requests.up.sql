CREATE TABLE IF NOT EXISTS org_join_requests (
    id TEXT PRIMARY KEY,
    org_id TEXT NOT NULL,
    user_email TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending',
    created_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_org_join_requests_org_id ON org_join_requests(org_id);
