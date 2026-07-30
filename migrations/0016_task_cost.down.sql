ALTER TABLE queued_tasks
  DROP COLUMN cost_usd,
  DROP COLUMN tokens_in,
  DROP COLUMN tokens_out,
  DROP COLUMN metered_at;
