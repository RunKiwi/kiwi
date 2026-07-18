ALTER TABLE audit_logs DROP COLUMN IF EXISTS user_email;
DROP TABLE IF EXISTS job_tokens;
DROP TABLE IF EXISTS task_events;
DROP TABLE IF EXISTS task_states;
DROP TABLE IF EXISTS org_provider_configs;
DROP TABLE IF EXISTS api_keys;
DROP TABLE IF EXISTS users;
