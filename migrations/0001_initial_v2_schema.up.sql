-- Tenancy & governance -------------------------------------------------
CREATE TABLE organizations (
  id            TEXT PRIMARY KEY,
  name          TEXT NOT NULL,
  created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE org_limits (
  org_id                 TEXT PRIMARY KEY REFERENCES organizations(id),
  max_concurrent_jobs    INT     NOT NULL DEFAULT 10,
  max_budget_per_job     NUMERIC NOT NULL DEFAULT 5.00,
  max_budget_per_month   NUMERIC NOT NULL DEFAULT 500.00,
  max_workers_per_job    INT     NOT NULL DEFAULT 8,
  task_timeout_seconds   INT     NOT NULL DEFAULT 1800,
  max_sandbox_disk_mb    INT     NOT NULL DEFAULT 2048
);

-- Workflows / templates (versioned, user- or default-authored) ---------
CREATE TABLE workflows (
  id            TEXT PRIMARY KEY,
  org_id        TEXT REFERENCES organizations(id),   -- NULL = built-in default
  name          TEXT NOT NULL,
  version       INT  NOT NULL,
  spec          JSONB NOT NULL,        -- master/worker model defaults, tool policy, AGENT.md refs
  created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (org_id, name, version)
);

-- Manifests (immutable, content-addressed) -----------------------------
CREATE TABLE manifests (
  id            TEXT PRIMARY KEY,      -- = sha256(content), content-addressed
  org_id        TEXT NOT NULL REFERENCES organizations(id),
  workflow_id   TEXT REFERENCES workflows(id),
  schema_version TEXT NOT NULL,
  content       JSONB NOT NULL,        -- master/workers, model info, tools, egress allowlist, limits
  producer      TEXT NOT NULL,         -- 'default' | 'user_template' | 'plugin:<name>'
  signature     TEXT,                  -- optional; validates producer authenticity
  created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Jobs (source of truth for scheduling; strongly consistent) -----------
CREATE TABLE jobs (
  id              TEXT PRIMARY KEY,
  org_id          TEXT NOT NULL REFERENCES organizations(id),
  user_id         TEXT NOT NULL,
  workflow_id     TEXT REFERENCES workflows(id),
  manifest_id     TEXT REFERENCES manifests(id),      -- set once manifest is generated
  status          TEXT NOT NULL,       -- PENDING|SCHEDULING|RUNNING|PAUSED|SUCCEEDED|FAILED|CANCELED
  idempotency_key TEXT,
  inputs          JSONB NOT NULL,
  sandbox_ref     TEXT,                -- provider handle
  cost_usd        NUMERIC NOT NULL DEFAULT 0,
  error           TEXT,
  created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (org_id, idempotency_key)     -- CP invariant: dedupe submissions
);
CREATE INDEX ON jobs (org_id, status);
CREATE INDEX ON jobs (status) WHERE status IN ('PENDING','SCHEDULING','RUNNING','PAUSED');

-- Agents within a job (master + workers) -------------------------------
CREATE TABLE agents (
  id          TEXT PRIMARY KEY,
  job_id      TEXT NOT NULL REFERENCES jobs(id),
  role        TEXT NOT NULL,           -- 'master' | 'worker'
  model       TEXT NOT NULL,
  status      TEXT NOT NULL,
  created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX ON agents (job_id);

-- Transactional outbox (exactly-once queue handoff) --------------------
CREATE TABLE outbox (
  id           BIGSERIAL PRIMARY KEY,
  job_id       TEXT NOT NULL REFERENCES jobs(id),
  topic        TEXT NOT NULL,
  payload      JSONB NOT NULL,
  published_at TIMESTAMPTZ,            -- NULL until relay publishes
  created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX ON outbox (published_at) WHERE published_at IS NULL;

-- Event log (append-only execution trace; the replay spine) ------------
CREATE TABLE events (
  id           BIGSERIAL PRIMARY KEY,
  job_id       TEXT NOT NULL REFERENCES jobs(id),
  agent_id     TEXT REFERENCES agents(id),
  seq          BIGINT NOT NULL,        -- per-job monotonic ordering
  phase        TEXT NOT NULL,          -- plan|actor|critic|tool_call|tool_result|message|status
  payload      JSONB NOT NULL,         -- small; large blobs → object store, referenced by URI
  trace_id     TEXT,                   -- W3C trace id for correlation
  span_id      TEXT,
  created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (job_id, seq)
);
CREATE INDEX ON events (job_id, seq);

-- Checkpoints (metadata in DB; workspace snapshot in object store) ------
CREATE TABLE checkpoints (
  id             TEXT PRIMARY KEY,
  job_id         TEXT NOT NULL REFERENCES jobs(id),
  agent_id       TEXT REFERENCES agents(id),
  event_seq      BIGINT NOT NULL,      -- event offset this checkpoint corresponds to
  snapshot_uri   TEXT,                 -- s3://… workspace snapshot; NULL = metadata-only ckpt
  snapshot_hash  TEXT,
  state          JSONB NOT NULL,       -- cursor / agent memory pointers / step counters
  created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (job_id, event_seq)
);
CREATE INDEX ON checkpoints (job_id, event_seq DESC);

-- Side-effect idempotency ledger (replay-safety for external effects) --
CREATE TABLE side_effects (
  id           TEXT PRIMARY KEY,       -- deterministic key: hash(job_id, step, effect_signature)
  job_id       TEXT NOT NULL REFERENCES jobs(id),
  effect_type  TEXT NOT NULL,          -- 'http'|'git_push'|'email'|…
  result_uri   TEXT,                   -- cached result so replay returns it instead of re-firing
  committed_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Audit + cost ---------------------------------------------------------
CREATE TABLE audit_logs (
  id BIGSERIAL PRIMARY KEY, 
  org_id TEXT, 
  user_id TEXT, 
  action TEXT, 
  resource TEXT,
  resource_id TEXT, 
  details TEXT, 
  client_ip TEXT, 
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
