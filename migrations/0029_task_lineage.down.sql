DROP INDEX IF EXISTS idx_queued_tasks_trigger_comment;
DROP INDEX IF EXISTS idx_queued_tasks_root;
DROP INDEX IF EXISTS idx_queued_tasks_parent;

ALTER TABLE queued_tasks DROP COLUMN IF EXISTS trigger_comment_id;
ALTER TABLE queued_tasks DROP COLUMN IF EXISTS origin;
ALTER TABLE queued_tasks DROP COLUMN IF EXISTS root_task_id;
ALTER TABLE queued_tasks DROP COLUMN IF EXISTS parent_task_id;
