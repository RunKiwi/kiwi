DROP INDEX IF EXISTS idx_execution_records_org_job_ver;

ALTER TABLE execution_records ADD CONSTRAINT execution_records_org_id_job_id_key UNIQUE (org_id, job_id);
