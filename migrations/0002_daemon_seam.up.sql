-- Daemon seam (issue #115): Data Plane <-> Control Plane handoff.
--
-- Also closes the schema drift called out in #115: queued_tasks and credentials
-- were introduced as GORM models and only ever created via AutoMigrate, despite
-- migrations/0001 claiming to be the production source of truth. They are
-- created here so a migrated database matches an AutoMigrated one.

-- Lease-based work queue (BYOC daemon handoff) -------------------------
-- Tasks are leased, not destructively popped: a crashed daemon's work returns
-- to the queue when its lease lapses. lease_id is a fencing token — every
-- renew/complete must present it, so a stale daemon cannot mutate a task whose
-- lease has since been reassigned.
CREATE TABLE IF NOT EXISTS queued_tasks (
  id                TEXT PRIMARY KEY,
  org_id            TEXT NOT NULL,
  job_id            TEXT,
  status            TEXT NOT NULL,          -- QUEUED|LEASED|SUCCEEDED|FAILED
  spec              JSONB NOT NULL,         -- the worker-spec.json payload
  leased_by         TEXT,                   -- daemon id holding the lease
  lease_id          TEXT,                   -- fencing token
  lease_expires_at  TIMESTAMPTZ,
  attempts          INT NOT NULL DEFAULT 0,
  created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_queued_tasks_org_id ON queued_tasks (org_id);
CREATE INDEX IF NOT EXISTS idx_queued_tasks_status ON queued_tasks (status);
CREATE INDEX IF NOT EXISTS idx_queued_tasks_lease_expires_at ON queued_tasks (lease_expires_at);
-- The lease hot path: find the oldest QUEUED task for an org.
CREATE INDEX IF NOT EXISTS idx_queued_tasks_lease_scan ON queued_tasks (org_id, status, created_at);

-- Org-scoped credentials -----------------------------------------------
-- Values are AES-256-GCM encrypted at rest; plaintext is never persisted. For
-- delivery they are re-sealed to a daemon's X25519 public key.
CREATE TABLE IF NOT EXISTS credentials (
  id               TEXT PRIMARY KEY,
  org_id           TEXT NOT NULL,
  name             TEXT NOT NULL,           -- env-var style, e.g. ANTHROPIC_API_KEY
  kind             TEXT NOT NULL,           -- llm|git
  encrypted_value  TEXT NOT NULL,
  created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (org_id, name)                     -- required by the ON CONFLICT upsert
);
CREATE INDEX IF NOT EXISTS idx_credentials_org_id ON credentials (org_id);

-- Registered Data Plane daemons ----------------------------------------
-- sign_pub_key (Ed25519) is the daemon's identity: heartbeats are signed with
-- the matching private key, and this row is what resolves a signed request to
-- an org. enc_pub_key (X25519) is the seal target for credential delivery.
CREATE TABLE IF NOT EXISTS daemons (
  id            TEXT PRIMARY KEY,
  org_id        TEXT NOT NULL REFERENCES organizations(id),
  sign_pub_key  TEXT NOT NULL UNIQUE,       -- base64 Ed25519 — the identity
  enc_pub_key   TEXT NOT NULL,              -- base64 X25519 — seal target
  last_seen_at  TIMESTAMPTZ,
  created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_daemons_org_id ON daemons (org_id);

-- Daemon join tokens ---------------------------------------------------
-- Short-lived, org-bound, single-use. Closes trust-on-first-use: without this,
-- "boot and self-register" would let anyone who finds the registration endpoint
-- enrol a rogue daemon and receive another org's tasks and sealed credentials.
-- Only the SHA-256 of the token is stored — a DB leak must not yield usable
-- join tokens.
CREATE TABLE IF NOT EXISTS daemon_join_tokens (
  token_hash  TEXT PRIMARY KEY,             -- hex(sha256(token))
  org_id      TEXT NOT NULL REFERENCES organizations(id),
  expires_at  TIMESTAMPTZ NOT NULL,
  used_at     TIMESTAMPTZ,                  -- non-null once redeemed (single-use)
  created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_daemon_join_tokens_org_id ON daemon_join_tokens (org_id);
