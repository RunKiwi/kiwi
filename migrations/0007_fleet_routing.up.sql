-- Fleet routing: bind daemons (and the join tokens that enrol them) to a fleet
-- so LeaseNextTask can scope a daemon to its fleet's work. Empty means the
-- org's unassigned pool.
ALTER TABLE daemons ADD COLUMN IF NOT EXISTS fleet_id TEXT NOT NULL DEFAULT '';
CREATE INDEX IF NOT EXISTS idx_daemons_fleet_id ON daemons (fleet_id);

ALTER TABLE daemon_join_tokens ADD COLUMN IF NOT EXISTS fleet_id TEXT NOT NULL DEFAULT '';
