DROP INDEX IF EXISTS idx_daemons_fleet_id;
ALTER TABLE daemons DROP COLUMN IF EXISTS fleet_id;
ALTER TABLE daemon_join_tokens DROP COLUMN IF EXISTS fleet_id;
