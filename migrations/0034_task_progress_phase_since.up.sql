-- phase_since: when the daemon's currently-reported phase started, so the
-- dashboard can show how long the current step has been running rather than
-- only that the feed is still alive (progress_at already answers the
-- latter). Same guard as 0020_task_progress_columns.up.sql, and for the same
-- reason: queued_tasks exists only via AutoMigrate, which is off in
-- production, so a GORM-struct-only field with no migration silently
-- diverges from the live schema until an INSERT/UPDATE naming the missing
-- column fails in prod. IF NOT EXISTS keeps this safe on a database where
-- AutoMigrate did run and already created it.
DO $$
BEGIN
  IF to_regclass('public.queued_tasks') IS NOT NULL THEN
    ALTER TABLE queued_tasks ADD COLUMN IF NOT EXISTS progress_phase_since TIMESTAMPTZ;
  END IF;
END $$;
