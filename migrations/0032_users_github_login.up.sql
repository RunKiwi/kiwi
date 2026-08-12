-- Keep the GitHub username captured at sign-in.
--
-- The callback already fetched `login` and used it only as a fallback for the
-- display name when the GitHub profile had none, then dropped it. What survived
-- was the numeric oauth_subject, which is stable and unguessable and answers no
-- question anyone asks: "which GitHub user is this?" required an API call, and
-- support could not match a person to an account at all.
--
-- Nullable, with no backfill, because there is nothing to backfill from — the
-- value was never stored. NULL therefore means both "not a GitHub user" and
-- "signed up before this column existed"; both are honestly "unknown", which is
-- what an empty string would have obscured.
--
-- Deliberately not unique. GitHub allows an account to be renamed and the freed
-- name to be re-registered by someone else, so a login is a label that can move
-- between people. oauth_subject remains the identity key; this is indexed only
-- so it can be looked up.
ALTER TABLE users ADD COLUMN IF NOT EXISTS github_login TEXT;

CREATE INDEX IF NOT EXISTS idx_users_github_login ON users (github_login);
