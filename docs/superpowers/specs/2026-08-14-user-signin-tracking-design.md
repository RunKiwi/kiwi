# User sign-in and dashboard activity tracking

Status: approved for implementation
Date: 2026-08-14

## Problem

There is no record today of how often a user signs in, when they last signed
in, or how long they're active in the dashboard. `ee/auth` authenticates
every request (OAuth login, session cookie, API key, bootstrap token) but
never records the event — `User` (`ee/auth/models.go`) carries no sign-in or
activity fields, and no table stores individual sign-in or session events.
Internal ops has no way to answer "how many times has this person signed
in," "when did we last see them," or "how long was their session."

## Goals

- Record how many times a user has signed in via OAuth, and when the most
  recent one was.
- Record when a user was last seen active in the dashboard, more current
  than "last sign-in" since one login cookie can span days of browsing.
- Define and record dashboard *sessions* (start, last activity, and
  therefore length) so ops can see activity spans, not just a single
  timestamp.
- Expose all of this through the existing superadmin `/admin` API surface.

## Non-goals

- **No frontend UI in this pass.** The data is reachable via the existing
  `/admin` API surface (curl/Postman-level), the same way audit logs and
  per-user cost usage are today. Revisit if internal ops wants it rendered
  in the `/admin` dashboard page.
- **CLI/SDK activity (API-key-authenticated requests) is not tracked.**
  "Signed in" and "session" here mean the browser dashboard only. API keys
  are long-lived, reused across CLI/SDK/daemon calls for weeks, and
  instrumenting `AuthMiddleware`'s API-key branch would mean writing on
  every Control-Plane request including daemon polling — a different,
  much higher-volume problem that isn't what was asked for.
- **No real logout tracking.** The session cookie
  (`ee/auth/oauth.go:CreateSessionCookieValue`) is stateless — HMAC-signed,
  fixed 7-day expiry, no server-side record, no logout endpoint. Rather than
  build logout tracking to get an exact login-to-logout span, session length
  is derived from observed activity with a 30-minute inactivity cutoff (see
  Design). This is the same sessionization approach analytics tools like GA
  use for exactly this reason.

## Design

### Data model

**Three new columns on `users`** (`ee/auth/models.go`):

```go
SignInCount int        `json:"sign_in_count" gorm:"not null;default:0"`
LastSignInAt *time.Time `json:"last_sign_in_at"`
LastSeenAt   *time.Time `json:"last_seen_at"`
```

- `SignInCount` / `LastSignInAt`: bumped once per completed OAuth login
  (GitHub or Google). Exact — one event, one update.
- `LastSeenAt`: bumped on any cookie-authenticated dashboard request.
  More current than `LastSignInAt` — a user can be "seen" many times within
  one 7-day login.

**New table `dashboard_sessions`**, a new type `DashboardSession` in
`ee/auth`:

```go
type DashboardSession struct {
    ID             string    `gorm:"primaryKey"`
    UserID         string    `gorm:"index;not null"`
    OrgID          string    `gorm:"index;not null"`
    StartedAt      time.Time `gorm:"not null"`
    LastActivityAt time.Time `gorm:"not null"`
}
```

Named `DashboardSession`, not `Session` or `UserSession`, because
`store.AgentSession` (`pkg/store/session_models.go`) already exists and
means something entirely different — a task's Architect/Implementer run.
Reusing "session" for a human login would make `grep -r Session` return two
unrelated concepts under the same name.

**Sessionization rule:** a 30-minute inactivity gap closes a session.
On a qualifying dashboard request:
- If the user's most recent `DashboardSession` row has `LastActivityAt`
  within the last 30 minutes, extend it (`UPDATE ... SET last_activity_at
  = now()`).
- Otherwise, insert a new row (`ID` generated the same way `handleCreateUser`
  generates user IDs — `generateHexID` in `ee/auth/admin.go` — and
  `StartedAt = LastActivityAt = now()`).

Session length is computed at read time as `LastActivityAt - StartedAt`.
There is no explicit "session end" — a session is "current" if its
`LastActivityAt` is within the 30-minute window, "closed" otherwise.

### Instrumentation

**Sign-in count/timestamp** — `handleOAuthCallback` in `ee/auth/oauth.go`
already has exactly one place per login where the user row is resolved and
the session cookie is issued, covering both the new-user and
existing-user branches. `SignInCount` is incremented and `LastSignInAt` set
there, as part of the same `db.Create`/`db.Save` call already happening.

**Last-seen / dashboard_sessions** — `AuthMiddleware` and `AuthFunc`
(`ee/auth/auth.go`) each currently duplicate the same block: extract the
session cookie, verify it, look up the `User`. This is factored into one
shared helper (e.g. `resolveCookieUser(db, r) (*User, error)`) used by both,
removing the existing duplication. Activity recording is added to that
helper, after a successful resolution — so it fires only for
cookie-authenticated requests, never for API-key or bootstrap-token auth,
per the non-goal above.

To avoid a write on every request (the SPA polls the dashboard API), the
write is skipped if the most recent session's `LastActivityAt` is less than
60 seconds old. Worst case, `LastSeenAt`/`LastActivityAt` lag real activity
by under a minute — irrelevant at the granularity ops needs. The write is
best-effort: a failure to record activity must never fail the underlying
request.

### API

- `GET /admin/orgs/{orgID}/users` (existing, `handleListUsers`) needs no
  handler change — it serializes `User` directly via
  `json.NewEncoder(w).Encode(users)`, so `sign_in_count`, `last_sign_in_at`,
  `last_seen_at` appear automatically once the columns exist.
- New: `GET /admin/orgs/{orgID}/users/{userID}/sessions` — returns the
  user's most recent `DashboardSession` rows (limit 20, newest first), so
  ops can see session-length history rather than a single point-in-time
  value. Routed alongside the existing `/admin/orgs/{orgID}/users/{userID}/keys`
  pattern in `admin.go`'s `/admin/orgs/` dispatcher, gated by
  `authorizeOrgAccess` like its siblings (`/audit`, `/keys`, `/model_usage`).

### Migration

New `migrations/0035_user_signin_tracking.up.sql` (+ matching
`.down.sql`), following the pattern of `0032_users_github_login.up.sql`:

```sql
ALTER TABLE users ADD COLUMN IF NOT EXISTS sign_in_count INT NOT NULL DEFAULT 0;
ALTER TABLE users ADD COLUMN IF NOT EXISTS last_sign_in_at TIMESTAMPTZ;
ALTER TABLE users ADD COLUMN IF NOT EXISTS last_seen_at TIMESTAMPTZ;

CREATE TABLE IF NOT EXISTS dashboard_sessions (
    id               TEXT PRIMARY KEY,
    user_id          TEXT NOT NULL,
    org_id           TEXT NOT NULL,
    started_at       TIMESTAMPTZ NOT NULL,
    last_activity_at TIMESTAMPTZ NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_dashboard_sessions_user_started
    ON dashboard_sessions (user_id, started_at DESC);
```

`ee/auth/models.go`'s `InitAuthDB`/`OpenDB` (used by SQLite-backed tests)
gets `&DashboardSession{}` added to their `AutoMigrate` calls, matching how
every other auth table is registered there.

### Testing

- Extend `ee/auth/oauth_test.go`'s `githubSignIn` helper coverage: assert
  `SignInCount == 1` and `LastSignInAt` set after one login, `SignInCount
  == 2` and `LastSignInAt` advanced after a second login by the same user.
- Unit tests for the sessionization rule in isolation (extend vs. new vs.
  gap-close-and-start-new), driven by an injected clock rather than
  `time.Now()`, independent of HTTP.
- Handler test for `GET .../users/{userID}/sessions`: empty (no sessions
  yet), populated (returns rows newest-first), cross-org 403 (same pattern
  as the existing `/keys` endpoint's org-isolation test).
- A test asserting API-key-authenticated requests do **not** create or
  extend a `DashboardSession` or bump `LastSeenAt` — this is the guardrail
  for the "dashboard only" non-goal.
