-- Prompt cache token breakdown on queued_tasks.
--
-- Persists the split of tokens_in between cached prompt tokens and raw prompt tokens.
ALTER TABLE queued_tasks ADD COLUMN IF NOT EXISTS cached_prompt_tokens BIGINT NOT NULL DEFAULT 0;
ALTER TABLE queued_tasks ADD COLUMN IF NOT EXISTS raw_prompt_tokens BIGINT NOT NULL DEFAULT 0;
