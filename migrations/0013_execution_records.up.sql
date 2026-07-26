CREATE TABLE execution_records (
  record_id         TEXT PRIMARY KEY,
  org_id            TEXT NOT NULL,
  job_id            TEXT NOT NULL,
  ver               TEXT NOT NULL,
  prev_record_hash  TEXT NOT NULL DEFAULT '',
  record_hash       TEXT NOT NULL,
  body              JSONB NOT NULL,
  exec_signature    TEXT NOT NULL,
  record_signature  TEXT NOT NULL,
  signing_key_id    TEXT NOT NULL,
  created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (org_id, job_id)
);
CREATE INDEX idx_execution_records_org_created ON execution_records (org_id, created_at DESC);

CREATE TABLE execution_record_heads (
  org_id      TEXT PRIMARY KEY,
  head_hash   TEXT NOT NULL,
  updated_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
