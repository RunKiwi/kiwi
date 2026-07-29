-- Record why a provisioning request failed. A failed request is terminal and is
-- never retried, so previously the reason lived only in a log line on the
-- provisioning host: the org whose runner never started could not see it, and
-- its tasks sat QUEUED with no explanation until the queue TTL failed them.
ALTER TABLE provisioning_requests ADD COLUMN IF NOT EXISTS error TEXT NOT NULL DEFAULT '';
