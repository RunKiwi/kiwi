ALTER TABLE slack_installations ADD COLUMN encrypted_bot_token TEXT NOT NULL DEFAULT '';

-- Backfill from the legacy org-scoped credential so an installation that
-- connected before this migration keeps working without a re-install.
-- Same AES-256-GCM ciphertext either way, so it transfers as-is. An org
-- with two workspaces only ever had one surviving token there (the second
-- workspace's connect silently overwrote the first's row, the bug
-- EncryptedBotToken fixes going forward) — both rows get that one token,
-- no worse than before, and the next OAuth re-install corrects each.
UPDATE slack_installations
SET encrypted_bot_token = credentials.encrypted_value
FROM credentials
WHERE credentials.org_id = slack_installations.org_id
  AND credentials.name = 'SLACK_BOT_TOKEN'
  AND slack_installations.encrypted_bot_token = '';

CREATE TABLE slack_processed_events (
    event_id TEXT PRIMARY KEY,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
