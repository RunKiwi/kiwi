-- Tool arguments and a real event timestamp on task_events.
--
-- Two columns, for two different gaps in what a run could tell you about itself.
--
-- `input` carries a tool call's arguments as the model wrote them. Until now a
-- timeline could say that `run` was called and show what it printed, but never
-- the command — so "is the agent editing whole files or using sed?" was not
-- answerable from the record, only guessable from the shape of the output.
--
-- `created_at` already existed in practice: the GORM model declares it and
-- AutoMigrate (pkg/orchestrator/db.go) created the column, but no numbered
-- migration ever did, so a database built purely from this directory lacked it.
-- Declaring it here closes that drift. The daemon now stamps each event as it
-- happens rather than the Control Plane stamping a whole flush on arrival —
-- a flush carries several seconds of events, and one shared instant is exactly
-- the resolution needed to see where a run spends unaccounted time.
--
-- cost_usd is in the same position (model + AutoMigrate, never a migration), so
-- it is declared here too rather than left to drift further.
--
-- All three are IF NOT EXISTS: on any database AutoMigrate has already touched,
-- the columns are present and this is a no-op that records the intent.

ALTER TABLE task_events ADD COLUMN IF NOT EXISTS input TEXT NOT NULL DEFAULT '';
ALTER TABLE task_events ADD COLUMN IF NOT EXISTS cost_usd DOUBLE PRECISION NOT NULL DEFAULT 0;
ALTER TABLE task_events ADD COLUMN IF NOT EXISTS created_at TIMESTAMPTZ NOT NULL DEFAULT NOW();

-- Reading a run's timeline means "every event for this task, oldest first".
-- The existing index is on task_id alone, so that ordering was a sort.
CREATE INDEX IF NOT EXISTS idx_task_events_task_created
  ON task_events (task_id, created_at);
