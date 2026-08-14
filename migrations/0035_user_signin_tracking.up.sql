-- Track OAuth sign-ins and dashboard activity so internal ops can answer
-- "how many times has this person signed in," "when did we last see them,"
-- and "how long was their session" — none of which were recorded before.
--
-- sign_in_count / last_sign_in_at are bumped once per completed OAuth login
-- (ee/auth/oauth.go handleOAuthCallback) — a real, infrequent event, not
-- per-request activity. last_seen_at is bumped on any cookie-authenticated
-- dashboard request (ee/auth/dashboard_session.go recordDashboardActivity)
-- — more current than last_sign_in_at, since one login's cookie can span
-- days of browsing.
ALTER TABLE users ADD COLUMN IF NOT EXISTS sign_in_count INT NOT NULL DEFAULT 0;
ALTER TABLE users ADD COLUMN IF NOT EXISTS last_sign_in_at TIMESTAMPTZ;
ALTER TABLE users ADD COLUMN IF NOT EXISTS last_seen_at TIMESTAMPTZ;
