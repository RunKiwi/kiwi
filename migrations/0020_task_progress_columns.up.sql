-- Live progress columns for queued_tasks.
--
-- These were added to the QueuedTask GORM model without a migration, on the
-- assumption that AutoMigrate would create them. It does not: AutoMigrate runs
-- only when KIWI_AUTOMIGRATE=true (pkg/orchestrator/db.go), which production
-- does not set, and the `migrate` role runs RunMigrations alone. So the columns
-- existed in Go and nowhere in the database, and every enqueue failed:
--
--   ERROR: column "progress_phase" of relation "queued_tasks" does not exist
--
-- That is an INSERT failure on the planner's path, so it took out task
-- submission entirely rather than just the progress feature.
--
-- queued_tasks itself exists only through AutoMigrate (see the schema drift
-- note in CLAUDE.md §1), so this cannot assume the table is present — the
-- migrate role may run against a database no serving process has touched.
-- Guarded rather than assumed. IF NOT EXISTS on each column keeps it safe on a
-- database where AutoMigrate did run and already created them.
DO $$
BEGIN
  IF to_regclass('public.queued_tasks') IS NOT NULL THEN
    ALTER TABLE queued_tasks ADD COLUMN IF NOT EXISTS progress_phase  TEXT;
    ALTER TABLE queued_tasks ADD COLUMN IF NOT EXISTS progress_output TEXT;
    ALTER TABLE queued_tasks ADD COLUMN IF NOT EXISTS progress_at     TIMESTAMPTZ;

    -- Pre-existing drift of the same kind, found by the guard added with this
    -- change rather than by an outage. started_at backs agent-minute metering
    -- (pkg/store/queue.go meters from it, not UpdatedAt, because RenewLease
    -- resets UpdatedAt) and appears in no migration. Production has it because
    -- AutoMigrate created it in an earlier era; a database rebuilt from these
    -- migrations alone — disaster recovery, a new environment — would not, and
    -- leasing would fail exactly as enqueue does now.
    ALTER TABLE queued_tasks ADD COLUMN IF NOT EXISTS started_at      TIMESTAMPTZ;
  END IF;
END $$;
