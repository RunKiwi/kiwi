-- Which key paid for the work: 'byok' (the org's own) or 'kiwi' (a
-- Kiwi-owned platform key).
--
-- Recorded even when the resulting cost is zero, so "ran on our key" is a
-- stated fact rather than something inferred from a missing number. The spend
-- page reads this to keep Kiwi-funded work out of the dollar total the org owes.
ALTER TABLE jobs         ADD COLUMN IF NOT EXISTS funding TEXT NOT NULL DEFAULT 'byok';
ALTER TABLE queued_tasks ADD COLUMN IF NOT EXISTS funding TEXT NOT NULL DEFAULT 'byok';

CREATE INDEX IF NOT EXISTS idx_jobs_funding         ON jobs (org_id, funding);
CREATE INDEX IF NOT EXISTS idx_queued_tasks_funding ON queued_tasks (org_id, funding);
