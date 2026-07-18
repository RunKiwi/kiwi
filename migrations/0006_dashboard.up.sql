-- Fleets: named execution-capacity groups (self-managed | byoc).
CREATE TABLE IF NOT EXISTS fleets (
  id          TEXT PRIMARY KEY,
  org_id      TEXT NOT NULL,
  name        TEXT NOT NULL,
  type        TEXT NOT NULL,
  created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_fleets_org_id ON fleets (org_id);

-- Org-registered models available in the UI (beyond the built-in defaults).
CREATE TABLE IF NOT EXISTS org_models (
  id          TEXT PRIMARY KEY,
  org_id      TEXT NOT NULL,
  name        TEXT NOT NULL,
  provider    TEXT,
  created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_org_models_org_id ON org_models (org_id);

-- Optional fleet scoping for a task.
ALTER TABLE queued_tasks ADD COLUMN IF NOT EXISTS fleet_id TEXT;
CREATE INDEX IF NOT EXISTS idx_queued_tasks_fleet_id ON queued_tasks (fleet_id);
