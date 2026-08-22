ALTER TABLE daemons DROP COLUMN IF EXISTS last_cache_stats;
ALTER TABLE daemons DROP COLUMN IF EXISTS last_mem_stats;
ALTER TABLE daemons DROP COLUMN IF EXISTS active_containers;
