ALTER TABLE slack_channel_bindings ADD COLUMN default_model TEXT NOT NULL DEFAULT '';
ALTER TABLE slack_channel_bindings ADD COLUMN default_architect_model TEXT NOT NULL DEFAULT '';
