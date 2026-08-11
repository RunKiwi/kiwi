-- Task lineage: a review comment on a Kiwi pull request starts another task
-- that continues the same session, so "what happened here" is a thread rather
-- than a row.
--
-- queued_tasks is one of the tables that exists in production only through
-- AutoMigrate (see CLAUDE.md §1 on schema drift), so these columns are stated
-- here explicitly rather than left to it.

ALTER TABLE queued_tasks ADD COLUMN IF NOT EXISTS parent_task_id TEXT;
ALTER TABLE queued_tasks ADD COLUMN IF NOT EXISTS root_task_id TEXT;
ALTER TABLE queued_tasks ADD COLUMN IF NOT EXISTS origin TEXT NOT NULL DEFAULT 'submit';
ALTER TABLE queued_tasks ADD COLUMN IF NOT EXISTS trigger_comment_id BIGINT;

-- Every task that predates lineage is the root of its own thread. Without this
-- backfill a thread query returns nothing for them, which would read as "this
-- task never happened" rather than "this task has no children".
UPDATE queued_tasks SET root_task_id = id WHERE root_task_id IS NULL OR root_task_id = '';

CREATE INDEX IF NOT EXISTS idx_queued_tasks_parent ON queued_tasks (parent_task_id);
CREATE INDEX IF NOT EXISTS idx_queued_tasks_root ON queued_tasks (root_task_id);

-- Partial, because only continuations carry a comment id and NULLs must not
-- collide. GitHub redelivers webhooks; this is what stops a redelivery buying
-- the customer a second round.
CREATE UNIQUE INDEX IF NOT EXISTS idx_queued_tasks_trigger_comment
  ON queued_tasks (trigger_comment_id) WHERE trigger_comment_id IS NOT NULL;
