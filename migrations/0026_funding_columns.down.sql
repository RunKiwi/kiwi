DROP INDEX IF EXISTS idx_queued_tasks_funding;
DROP INDEX IF EXISTS idx_jobs_funding;
ALTER TABLE queued_tasks DROP COLUMN IF EXISTS funding;
ALTER TABLE jobs         DROP COLUMN IF EXISTS funding;
