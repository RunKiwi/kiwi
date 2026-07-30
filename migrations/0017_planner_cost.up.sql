ALTER TABLE jobs
  ADD COLUMN planner_cost_usd   DOUBLE PRECISION NOT NULL DEFAULT 0,
  ADD COLUMN planner_tokens_in  BIGINT           NOT NULL DEFAULT 0,
  ADD COLUMN planner_tokens_out BIGINT           NOT NULL DEFAULT 0;
