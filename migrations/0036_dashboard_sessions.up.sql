-- dashboard_sessions sessionizes cookie-authenticated dashboard activity
-- into spans, using a 30-minute inactivity gap (dashboardSessionGap in
-- ee/auth/dashboard_session.go) since the session cookie
-- (ee/auth/oauth.go CreateSessionCookieValue) is stateless — HMAC-signed,
-- fixed 7-day expiry, no server-side record, no logout endpoint — so there
-- is no exact "session end" event to bound a session by instead.
--
-- Named "dashboard_sessions", not "sessions" or "user_sessions", because it
-- tracks human dashboard logins. agent_sessions (pkg/store/session_models.go
-- store.AgentSession) already uses "session" for a task's
-- Architect/Implementer run — an unrelated concept — and the two names must
-- not collide.
CREATE TABLE IF NOT EXISTS dashboard_sessions (
    id               TEXT PRIMARY KEY,
    user_id          TEXT NOT NULL,
    org_id           TEXT NOT NULL,
    started_at       TIMESTAMPTZ NOT NULL,
    last_activity_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_dashboard_sessions_user_started
    ON dashboard_sessions (user_id, started_at DESC);
