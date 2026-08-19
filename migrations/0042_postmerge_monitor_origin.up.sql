-- migrations/0042_postmerge_monitor_origin.up.sql
--
-- Add origin column to distinguish Kiwi-authored monitors (job_id != '') from
-- external-PR monitors (job_id = ''). Fix the partial-unique index so multiple
-- external_pr monitors don't collide with each other.
ALTER TABLE postmerge_monitors ADD COLUMN origin TEXT NOT NULL DEFAULT 'kiwi_pr';
ALTER TABLE postmerge_monitors ALTER COLUMN job_id SET DEFAULT '';

-- Phase 1a's original unique index assumed job_id was always a real,
-- non-empty job. An external_pr monitor has no originating job at all —
-- job_id = '' for every one of them — so the uniqueness constraint must
-- only apply when job_id is a real job, or every second external_pr
-- monitor for the same org would collide with the first.
DROP INDEX IF EXISTS idx_postmerge_monitors_org_job;
CREATE UNIQUE INDEX idx_postmerge_monitors_org_job ON postmerge_monitors (org_id, job_id) WHERE job_id != '';
