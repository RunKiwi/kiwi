-- Reconcile missing tables from AutoMigrate:
-- users, api_keys, org_provider_configs, task_states, task_events, job_tokens

ALTER TABLE audit_logs ADD COLUMN IF NOT EXISTS user_email TEXT;

CREATE TABLE IF NOT EXISTS users (
  id         TEXT PRIMARY KEY,
  email      TEXT NOT NULL,
  name       TEXT,
  org_id     TEXT NOT NULL REFERENCES organizations(id),
  role       TEXT NOT NULL DEFAULT 'member',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_users_email ON users(email);
CREATE INDEX IF NOT EXISTS idx_users_org_id ON users(org_id);

CREATE TABLE IF NOT EXISTS api_keys (
  id         TEXT PRIMARY KEY,
  key_hash   TEXT NOT NULL,
  user_id    TEXT NOT NULL REFERENCES users(id),
  label      TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  expires_at TIMESTAMPTZ,
  revoked_at TIMESTAMPTZ
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_api_keys_key_hash ON api_keys(key_hash);
CREATE INDEX IF NOT EXISTS idx_api_keys_user_id ON api_keys(user_id);

CREATE TABLE IF NOT EXISTS org_provider_configs (
  org_id             TEXT PRIMARY KEY REFERENCES organizations(id),
  provider_name      TEXT NOT NULL,
  encrypted_api_key  TEXT NOT NULL,
  actor_model        TEXT NOT NULL,
  critic_model       TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_org_provider_configs_org_id ON org_provider_configs(org_id);

CREATE TABLE IF NOT EXISTS task_states (
  id              TEXT PRIMARY KEY,
  task            TEXT NOT NULL,
  file_path       TEXT NOT NULL,
  test_cmd        TEXT NOT NULL,
  status          TEXT NOT NULL,
  logs            TEXT NOT NULL,
  created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
  cost            NUMERIC NOT NULL DEFAULT 0,
  idempotency_key TEXT,
  user_id         TEXT NOT NULL,
  org_id          TEXT NOT NULL REFERENCES organizations(id),
  user_email      TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_task_states_idempotency_key ON task_states(idempotency_key);
CREATE INDEX IF NOT EXISTS idx_task_states_user_id ON task_states(user_id);
CREATE INDEX IF NOT EXISTS idx_task_states_org_id ON task_states(org_id);

CREATE TABLE IF NOT EXISTS task_events (
  id             BIGSERIAL PRIMARY KEY,
  task_id        TEXT NOT NULL REFERENCES task_states(id),
  org_id         TEXT NOT NULL REFERENCES organizations(id),
  step           INT NOT NULL,
  phase          TEXT NOT NULL,
  outcome        TEXT NOT NULL,
  detail         TEXT NOT NULL,
  duration_ms    BIGINT NOT NULL,
  input_tokens   BIGINT NOT NULL,
  output_tokens  BIGINT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_task_events_task_id ON task_events(task_id);
CREATE INDEX IF NOT EXISTS idx_task_events_org_id ON task_events(org_id);

CREATE TABLE IF NOT EXISTS job_tokens (
  id         TEXT PRIMARY KEY,
  token_hash TEXT NOT NULL,
  job_id     TEXT NOT NULL REFERENCES jobs(id),
  org_id     TEXT NOT NULL REFERENCES organizations(id),
  expires_at TIMESTAMPTZ NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_job_tokens_token_hash ON job_tokens(token_hash);
CREATE INDEX IF NOT EXISTS idx_job_tokens_job_id ON job_tokens(job_id);
