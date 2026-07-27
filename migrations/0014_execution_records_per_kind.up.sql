-- A job now holds more than one record: the execution record (kiwi.ver/v1) and,
-- once its PR merges, a merge record (kiwi.ver/merge/v1). Widen the uniqueness
-- to include the kind rather than dropping it.
--
-- The constraint is what actually prevents a duplicate append: the existence
-- check inside AppendExecutionRecord runs in its own transaction and cannot see
-- a concurrent pending insert, so without this two racing appends both commit.
ALTER TABLE execution_records DROP CONSTRAINT IF EXISTS execution_records_org_id_job_id_key;

CREATE UNIQUE INDEX IF NOT EXISTS idx_execution_records_org_job_ver
  ON execution_records (org_id, job_id, ver);
