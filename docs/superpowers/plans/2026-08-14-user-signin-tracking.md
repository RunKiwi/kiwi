# User Sign-In and Dashboard Activity Tracking Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Record how many times a user has signed in via OAuth, when they last signed in, when they were last seen active in the dashboard, and sessionized spans of that activity — exposed through the existing superadmin `/admin` API.

**Architecture:** Three new columns on `users` (`sign_in_count`, `last_sign_in_at`, `last_seen_at`) plus a new `dashboard_sessions` table, both written from `ee/auth`. Sign-in counting hooks into the single point in the OAuth callback where the logged-in user is resolved. Activity/session tracking hooks into the session-cookie branch shared by `AuthMiddleware` and `AuthFunc` — never the API-key branch, so CLI/SDK/daemon traffic is never counted. A new `GET /admin/orgs/{orgID}/users/{userID}/sessions` endpoint exposes session history; the existing `GET /admin/orgs/{orgID}/users` endpoint picks up the new `User` fields automatically since it serializes the struct directly.

**Tech Stack:** Go, GORM (Postgres in prod, SQLite in tests), raw SQL migrations under `migrations/` (embedded, applied by `RunMigrations`).

## Global Constraints

- `gofmt -l cmd/ pkg/ ee/` must print nothing.
- `CGO_ENABLED=0 go vet ./...` must be clean.
- `CGO_ENABLED=0 go test ./pkg/...` must pass (repo-wide mandate); this plan's own tests live in `ee/auth`, so also run `CGO_ENABLED=0 go test ./ee/...`.
- `CGO_ENABLED=0 go build ./...` must build all packages.
- Production runs numbered SQL migrations from `migrations/*.up.sql`, **not** GORM `AutoMigrate` (`KIWI_AUTOMIGRATE` is unset in prod) — every new/changed column needs a migration file, or it exists in Go and nowhere in the database. This is not a style preference; a past outage (`ee/orchestrator/schema_drift_test.go`'s header comment) was exactly this mistake on a different table.
- `ee/` is Business Source License 1.1 code; this plan only touches `ee/auth` and `migrations/`, both already `ee`-side or license-neutral, so the Apache/BSL boundary (`pkg/licensing_boundary_test.go`) is not at risk.
- New sessions/tracking concepts must be named `DashboardSession`, never `Session`/`UserSession` — `store.AgentSession` (`pkg/store/session_models.go`) already uses "session" for a task's Architect/Implementer run, an unrelated concept.
- Activity tracking (session creation, `last_seen_at`) covers **cookie-authenticated dashboard requests only** — never API-key or bootstrap-token auth. This is a hard scope boundary agreed in the design, not an oversight to "complete" later.
- All new DB writes for activity tracking are best-effort: a failure to record activity must never fail the underlying request.

---

## Task 1: Sign-in columns on `users`

**Files:**
- Create: `migrations/0035_user_signin_tracking.up.sql`
- Create: `migrations/0035_user_signin_tracking.down.sql`
- Modify: `ee/auth/models.go` (add 3 fields to `User`)
- Create: `ee/auth/schema_drift_test.go`
- Modify: `ee/auth/models_test.go` (add round-trip test)

**Interfaces:**
- Produces: `User.SignInCount int`, `User.LastSignInAt *time.Time`, `User.LastSeenAt *time.Time` (JSON tags `sign_in_count`, `last_sign_in_at`, `last_seen_at`) — consumed by Task 4 (oauth.go), Task 5 (auth.go / dashboard_session.go), and the existing `handleListUsers` in `admin.go` (no change needed there — it serializes `User` directly).

- [ ] **Step 1: Write the migration SQL**

Create `migrations/0035_user_signin_tracking.up.sql`:

```sql
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
```

Create `migrations/0035_user_signin_tracking.down.sql`:

```sql
ALTER TABLE users DROP COLUMN IF EXISTS last_seen_at;
ALTER TABLE users DROP COLUMN IF EXISTS last_sign_in_at;
ALTER TABLE users DROP COLUMN IF EXISTS sign_in_count;
```

These are picked up automatically — `migrations/embed.go` embeds `*.sql` by glob, no registry to update.

- [ ] **Step 2: Add the fields to the `User` struct**

In `ee/auth/models.go`, the `User` struct currently ends:

```go
	GitHubLogin *string   `json:"github_login,omitempty" gorm:"column:github_login;index"`
	CreatedAt   time.Time `json:"created_at"`
}
```

Change it to:

```go
	GitHubLogin *string   `json:"github_login,omitempty" gorm:"column:github_login;index"`
	CreatedAt   time.Time `json:"created_at"`
	// SignInCount and LastSignInAt are bumped once per completed OAuth login
	// (ee/auth/oauth.go handleOAuthCallback) — a real, infrequent event, not
	// per-request activity.
	SignInCount  int        `json:"sign_in_count" gorm:"not null;default:0"`
	LastSignInAt *time.Time `json:"last_sign_in_at"`
	// LastSeenAt is set by recordDashboardActivity (dashboard_session.go)
	// from cookie-authenticated requests only — API keys (CLI/SDK/daemon
	// traffic) never touch it, so it reflects browser dashboard use, and is
	// more current than LastSignInAt since one login's cookie can span days.
	LastSeenAt *time.Time `json:"last_seen_at"`
}
```

No change is needed to `InitAuthDB`/`OpenDB`'s `AutoMigrate` calls — `&User{}` is already in both lists, so the new fields ride along.

- [ ] **Step 3: Write the schema-drift guard test**

This mirrors `ee/orchestrator/schema_drift_test.go`'s `TestQueuedTaskColumnsExistInMigrations`, self-contained in `ee/auth` (no import of `ee/orchestrator` — that would be a backwards dependency; `ee/orchestrator` imports `ee/auth`, not the reverse).

Create `ee/auth/schema_drift_test.go`:

```go
// SPDX-License-Identifier: LicenseRef-Kiwi-BSL-1.1
// Copyright (c) 2026 RunKiwi. Licensed under the Business Source License 1.1.
// See ee/LICENSE. This is Control Plane code and is NOT Apache-2.0.

package auth

import (
	"reflect"
	"strings"
	"testing"

	"github.com/ibreakthecloud/kiwi/migrations"
	"gorm.io/gorm/schema"
)

// Every column a live model declares must exist in the numbered migrations.
//
// Production runs numbered SQL migrations only (KIWI_AUTOMIGRATE is unset),
// never GORM AutoMigrate — see ee/orchestrator/schema_drift_test.go for the
// outage that made this the rule. This is the ee/auth-side guard for the
// same mistake: a field added to User or DashboardSession with no migration
// behind it would exist in Go and nowhere in the database.
func TestUserColumnsExistInMigrations(t *testing.T) {
	assertColumnsInMigrations(t, User{})
}

func assertColumnsInMigrations(t *testing.T, model interface{}) {
	t.Helper()
	sql := allMigrationSQL(t)
	ns := schema.NamingStrategy{}

	rt := reflect.TypeOf(model)
	for i := 0; i < rt.NumField(); i++ {
		f := rt.Field(i)
		if !f.IsExported() {
			continue
		}
		tag := f.Tag.Get("gorm")
		if strings.Contains(tag, "-") && strings.HasPrefix(strings.TrimSpace(tag), "-") {
			continue // explicitly not a column
		}

		col := columnFromGormTag(tag)
		if col == "" {
			col = ns.ColumnName("", f.Name)
		}
		if !strings.Contains(sql, col) {
			t.Errorf("%s.%s maps to column %q, which appears in no migration.\n"+
				"Production does not run AutoMigrate (KIWI_AUTOMIGRATE is unset), so a column that "+
				"exists only in the Go model does not exist in the database, and every insert fails.\n"+
				"Add a numbered migration in migrations/.", rt.Name(), f.Name, col)
		}
	}
}

func columnFromGormTag(tag string) string {
	for _, part := range strings.Split(tag, ";") {
		part = strings.TrimSpace(part)
		if strings.HasPrefix(part, "column:") {
			return strings.TrimPrefix(part, "column:")
		}
	}
	return ""
}

func allMigrationSQL(t *testing.T) string {
	t.Helper()
	entries, err := migrations.FS.ReadDir(".")
	if err != nil {
		t.Fatalf("read migrations: %v", err)
	}
	var b strings.Builder
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		data, err := migrations.FS.ReadFile(e.Name())
		if err != nil {
			t.Fatalf("read %s: %v", e.Name(), err)
		}
		b.Write(data)
		b.WriteByte('\n')
	}
	if b.Len() == 0 {
		t.Fatal("no migrations found; the guard would pass vacuously")
	}
	return b.String()
}
```

(`assertColumnsInMigrations` is written to take any model so Task 2 can add `TestDashboardSessionColumnsExistInMigrations(t) { assertColumnsInMigrations(t, DashboardSession{}) }` without duplicating the reflection walk.)

- [ ] **Step 4: Run the drift test to verify it passes**

Run: `CGO_ENABLED=0 go test ./ee/auth/... -run TestUserColumnsExistInMigrations -v`
Expected: PASS

- [ ] **Step 5: Write the round-trip test**

In `ee/auth/models_test.go`, add after `TestOrganization_Defaults`:

```go
func TestUser_SignInFieldsDefaultAndRoundTrip(t *testing.T) {
	db := setupTestDB(t)

	org := Organization{ID: "org-signin", Name: "SignIn Org"}
	if err := db.Create(&org).Error; err != nil {
		t.Fatalf("create org: %v", err)
	}
	user := User{ID: "user-signin", Email: "signin@test.com", Name: "SignIn User", OrgID: org.ID, Role: "member"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}

	var fresh User
	if err := db.First(&fresh, "id = ?", user.ID).Error; err != nil {
		t.Fatalf("reload user: %v", err)
	}
	if fresh.SignInCount != 0 {
		t.Errorf("expected sign_in_count to default to 0, got %d", fresh.SignInCount)
	}
	if fresh.LastSignInAt != nil || fresh.LastSeenAt != nil {
		t.Errorf("expected last_sign_in_at and last_seen_at to default to nil, got %+v / %+v", fresh.LastSignInAt, fresh.LastSeenAt)
	}

	now := time.Now().Truncate(time.Second)
	fresh.SignInCount = 3
	fresh.LastSignInAt = &now
	fresh.LastSeenAt = &now
	if err := db.Save(&fresh).Error; err != nil {
		t.Fatalf("save user: %v", err)
	}

	var reloaded User
	if err := db.First(&reloaded, "id = ?", user.ID).Error; err != nil {
		t.Fatalf("reload user again: %v", err)
	}
	if reloaded.SignInCount != 3 {
		t.Errorf("expected sign_in_count 3, got %d", reloaded.SignInCount)
	}
	if reloaded.LastSignInAt == nil || !reloaded.LastSignInAt.Equal(now) {
		t.Errorf("expected last_sign_in_at %v, got %v", now, reloaded.LastSignInAt)
	}
	if reloaded.LastSeenAt == nil || !reloaded.LastSeenAt.Equal(now) {
		t.Errorf("expected last_seen_at %v, got %v", now, reloaded.LastSeenAt)
	}
}
```

`models_test.go` currently has `import "testing"` as a single unparenthesized import. Change it to:

```go
import (
	"testing"
	"time"
)
```

- [ ] **Step 6: Run the round-trip test to verify it passes**

Run: `CGO_ENABLED=0 go test ./ee/auth/... -run TestUser_SignInFieldsDefaultAndRoundTrip -v`
Expected: PASS

- [ ] **Step 7: Run gofmt and vet**

Run: `gofmt -l ee/auth/ migrations/ && CGO_ENABLED=0 go vet ./ee/auth/...`
Expected: no output from gofmt, clean vet

- [ ] **Step 8: Commit**

```bash
git add migrations/0035_user_signin_tracking.up.sql migrations/0035_user_signin_tracking.down.sql \
        ee/auth/models.go ee/auth/schema_drift_test.go ee/auth/models_test.go
git commit -m "feat(auth): add sign-in tracking columns to users"
```

---

## Task 2: `DashboardSession` model and table

**Files:**
- Create: `migrations/0036_dashboard_sessions.up.sql`
- Create: `migrations/0036_dashboard_sessions.down.sql`
- Create: `ee/auth/dashboard_session.go`
- Modify: `ee/auth/models.go` (register `DashboardSession` with `InitAuthDB`/`OpenDB`)
- Modify: `ee/auth/schema_drift_test.go` (add drift test for the new type)
- Create: `ee/auth/dashboard_session_test.go`

**Interfaces:**
- Consumes: nothing new from Task 1 beyond `User` already having `OrgID`.
- Produces: `type DashboardSession struct { ID, UserID, OrgID string; StartedAt, LastActivityAt time.Time }`, `func (DashboardSession) TableName() string` — consumed by Task 3 (`recordDashboardActivity`) and Task 6 (the sessions-listing endpoint).

- [ ] **Step 1: Write the migration SQL**

Create `migrations/0036_dashboard_sessions.up.sql`:

```sql
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
```

Create `migrations/0036_dashboard_sessions.down.sql`:

```sql
DROP INDEX IF EXISTS idx_dashboard_sessions_user_started;
DROP TABLE IF EXISTS dashboard_sessions;
```

- [ ] **Step 2: Write the `DashboardSession` model**

Create `ee/auth/dashboard_session.go`:

```go
// SPDX-License-Identifier: LicenseRef-Kiwi-BSL-1.1
// Copyright (c) 2026 RunKiwi. Licensed under the Business Source License 1.1.
// See ee/LICENSE. This is Control Plane code and is NOT Apache-2.0.

package auth

import (
	"time"
)

// DashboardSession is a sessionized span of browser-dashboard activity for a
// user, derived from cookie-authenticated requests. It is deliberately not
// named Session/UserSession: store.AgentSession
// (pkg/store/session_models.go) already uses "session" for a task's
// Architect/Implementer run, an unrelated concept, and reusing the word here
// would make it ambiguous which one a caller means.
//
// There is no explicit "end" event — the session cookie is stateless and has
// no logout endpoint (see CreateSessionCookieValue in oauth.go). A session
// is "current" if LastActivityAt is within dashboardSessionGap of now, and
// "closed" otherwise; length is LastActivityAt - StartedAt, computed at read
// time rather than stored.
type DashboardSession struct {
	ID             string    `json:"id" gorm:"primaryKey"`
	UserID         string    `json:"user_id" gorm:"index;not null"`
	OrgID          string    `json:"org_id" gorm:"index;not null"`
	StartedAt      time.Time `json:"started_at" gorm:"not null"`
	LastActivityAt time.Time `json:"last_activity_at" gorm:"not null"`
}

// TableName overrides the default GORM table name.
func (DashboardSession) TableName() string { return "dashboard_sessions" }
```

- [ ] **Step 3: Register the model with `InitAuthDB` and `OpenDB`**

In `ee/auth/models.go`, `InitAuthDB` currently reads:

```go
func InitAuthDB(db *gorm.DB) error {
	return db.AutoMigrate(&Organization{}, &User{}, &APIKey{}, &OrgLimits{}, &OrgProviderConfig{}, &OrgJoinRequest{}, &ProvisioningRequest{}, &store.Fleet{})
}
```

Change to:

```go
func InitAuthDB(db *gorm.DB) error {
	return db.AutoMigrate(&Organization{}, &User{}, &APIKey{}, &OrgLimits{}, &OrgProviderConfig{}, &OrgJoinRequest{}, &ProvisioningRequest{}, &store.Fleet{}, &DashboardSession{})
}
```

`OpenDB` (same file) has two separate `AutoMigrate` calls: one for the auth models (identical in content to `InitAuthDB`'s), one for `additionalModels...` (a variadic parameter, unrelated — leave that one alone). It's the **first** one to change:

```go
	// Migrate auth models
	if err := db.AutoMigrate(&Organization{}, &User{}, &APIKey{}, &OrgLimits{}, &OrgProviderConfig{}, &OrgJoinRequest{}, &ProvisioningRequest{}, &store.Fleet{}); err != nil {
		return nil, err
	}
```

becomes:

```go
	// Migrate auth models
	if err := db.AutoMigrate(&Organization{}, &User{}, &APIKey{}, &OrgLimits{}, &OrgProviderConfig{}, &OrgJoinRequest{}, &ProvisioningRequest{}, &store.Fleet{}, &DashboardSession{}); err != nil {
		return nil, err
	}
```

Do **not** touch the later `if len(additionalModels) > 0 { ... AutoMigrate(additionalModels...) }` block — that one migrates whatever the caller passed in, not the fixed auth model list.

- [ ] **Step 4: Add the drift-guard test for `DashboardSession`**

In `ee/auth/schema_drift_test.go`, add below `TestUserColumnsExistInMigrations`:

```go
func TestDashboardSessionColumnsExistInMigrations(t *testing.T) {
	assertColumnsInMigrations(t, DashboardSession{})
}
```

- [ ] **Step 5: Run the drift test to verify it passes**

Run: `CGO_ENABLED=0 go test ./ee/auth/... -run TestDashboardSessionColumnsExistInMigrations -v`
Expected: PASS

- [ ] **Step 6: Write a create/query round-trip test**

Create `ee/auth/dashboard_session_test.go`:

```go
// SPDX-License-Identifier: LicenseRef-Kiwi-BSL-1.1
// Copyright (c) 2026 RunKiwi. Licensed under the Business Source License 1.1.
// See ee/LICENSE. This is Control Plane code and is NOT Apache-2.0.

package auth

import (
	"testing"
	"time"
)

func TestDashboardSession_CreateAndQuery(t *testing.T) {
	db := setupTestDB(t)

	org := Organization{ID: "org-ds", Name: "DS Org"}
	if err := db.Create(&org).Error; err != nil {
		t.Fatalf("create org: %v", err)
	}
	user := User{ID: "user-ds", Email: "ds@test.com", Name: "DS User", OrgID: org.ID, Role: "member"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}

	now := time.Now().Truncate(time.Second)
	sess := DashboardSession{ID: "dsess_1", UserID: user.ID, OrgID: org.ID, StartedAt: now, LastActivityAt: now}
	if err := db.Create(&sess).Error; err != nil {
		t.Fatalf("create session: %v", err)
	}

	var fetched DashboardSession
	if err := db.Where("user_id = ?", user.ID).First(&fetched).Error; err != nil {
		t.Fatalf("query session: %v", err)
	}
	if fetched.ID != sess.ID || fetched.OrgID != org.ID {
		t.Errorf("session mismatch: %+v", fetched)
	}
	if !fetched.StartedAt.Equal(now) || !fetched.LastActivityAt.Equal(now) {
		t.Errorf("timestamp mismatch: started=%v last=%v want=%v", fetched.StartedAt, fetched.LastActivityAt, now)
	}
}
```

- [ ] **Step 7: Run the round-trip test to verify it passes**

Run: `CGO_ENABLED=0 go test ./ee/auth/... -run TestDashboardSession_CreateAndQuery -v`
Expected: PASS

- [ ] **Step 8: Run gofmt and vet**

Run: `gofmt -l ee/auth/ migrations/ && CGO_ENABLED=0 go vet ./ee/auth/...`
Expected: no output from gofmt, clean vet

- [ ] **Step 9: Commit**

```bash
git add migrations/0036_dashboard_sessions.up.sql migrations/0036_dashboard_sessions.down.sql \
        ee/auth/dashboard_session.go ee/auth/dashboard_session_test.go \
        ee/auth/models.go ee/auth/schema_drift_test.go
git commit -m "feat(auth): add DashboardSession model and table"
```

---

## Task 3: Sessionization logic (`resolveCookieUser`, `recordDashboardActivity`)

**Files:**
- Modify: `ee/auth/dashboard_session.go` (add the two functions, constants, and clock var)
- Modify: `ee/auth/dashboard_session_test.go` (add sessionization tests)

**Interfaces:**
- Consumes: `DashboardSession` (Task 2), `User` (Task 1), `store.NewDashID(prefix string) string` (`pkg/store`, already imported elsewhere in `ee/auth` — e.g. `auth.go`'s `CreateDefaultFleet`), `generateHexID` is **not** used here (superseded by `store.NewDashID` for consistency with the rest of the package's ID generation).
- Produces:
  - `func resolveCookieUser(db *gorm.DB, r *http.Request) *User` — returns nil if there is no valid session cookie.
  - `func recordDashboardActivity(db *gorm.DB, user *User)` — best-effort; never returns an error.
  - `var dashboardActivityClock func() time.Time` — package-level, overridable in tests, mirroring the existing `githubEndpoint`/`githubAPIURL` var-override pattern in `oauth_test.go`.
  These are consumed by Task 5 (`AuthMiddleware`, `AuthFunc`).

- [ ] **Step 1: Write the sessionization tests first**

Add to `ee/auth/dashboard_session_test.go`:

```go
func TestRecordDashboardActivity_StartsNewSessionWhenNoneExists(t *testing.T) {
	db := setupTestDB(t)
	org := Organization{ID: "org-rda1", Name: "RDA Org 1"}
	db.Create(&org)
	user := User{ID: "user-rda1", Email: "rda1@test.com", Name: "RDA User", OrgID: org.ID, Role: "member"}
	db.Create(&user)

	fixedNow := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)
	oldClock := dashboardActivityClock
	dashboardActivityClock = func() time.Time { return fixedNow }
	defer func() { dashboardActivityClock = oldClock }()

	recordDashboardActivity(db, &user)

	var sessions []DashboardSession
	if err := db.Where("user_id = ?", user.ID).Find(&sessions).Error; err != nil {
		t.Fatalf("query sessions: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(sessions))
	}
	if !sessions[0].StartedAt.Equal(fixedNow) || !sessions[0].LastActivityAt.Equal(fixedNow) {
		t.Errorf("expected session to start at %v, got started=%v last=%v", fixedNow, sessions[0].StartedAt, sessions[0].LastActivityAt)
	}

	var reloaded User
	db.First(&reloaded, "id = ?", user.ID)
	if reloaded.LastSeenAt == nil || !reloaded.LastSeenAt.Equal(fixedNow) {
		t.Errorf("expected last_seen_at %v, got %v", fixedNow, reloaded.LastSeenAt)
	}
}

func TestRecordDashboardActivity_ExtendsWithinGapAfterThrottleElapses(t *testing.T) {
	db := setupTestDB(t)
	org := Organization{ID: "org-rda2", Name: "RDA Org 2"}
	db.Create(&org)
	user := User{ID: "user-rda2", Email: "rda2@test.com", Name: "RDA User", OrgID: org.ID, Role: "member"}
	db.Create(&user)

	t0 := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)
	oldClock := dashboardActivityClock
	defer func() { dashboardActivityClock = oldClock }()

	dashboardActivityClock = func() time.Time { return t0 }
	recordDashboardActivity(db, &user)

	// 5 minutes later: within the 30-minute session gap, and past the 60s
	// write throttle, so the existing session must extend, not duplicate.
	t1 := t0.Add(5 * time.Minute)
	dashboardActivityClock = func() time.Time { return t1 }
	recordDashboardActivity(db, &user)

	var sessions []DashboardSession
	if err := db.Where("user_id = ?", user.ID).Find(&sessions).Error; err != nil {
		t.Fatalf("query sessions: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("expected the session to be extended in place, got %d rows", len(sessions))
	}
	if !sessions[0].StartedAt.Equal(t0) {
		t.Errorf("expected started_at to stay at %v, got %v", t0, sessions[0].StartedAt)
	}
	if !sessions[0].LastActivityAt.Equal(t1) {
		t.Errorf("expected last_activity_at to advance to %v, got %v", t1, sessions[0].LastActivityAt)
	}
}

func TestRecordDashboardActivity_SkipsWriteWithinThrottleWindow(t *testing.T) {
	db := setupTestDB(t)
	org := Organization{ID: "org-rda3", Name: "RDA Org 3"}
	db.Create(&org)
	user := User{ID: "user-rda3", Email: "rda3@test.com", Name: "RDA User", OrgID: org.ID, Role: "member"}
	db.Create(&user)

	t0 := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)
	oldClock := dashboardActivityClock
	defer func() { dashboardActivityClock = oldClock }()

	dashboardActivityClock = func() time.Time { return t0 }
	recordDashboardActivity(db, &user)

	// 10 seconds later: inside the 60s write-throttle window. The SPA polls
	// far faster than a session-length report needs, so this must be a
	// no-op, not a second write.
	t1 := t0.Add(10 * time.Second)
	dashboardActivityClock = func() time.Time { return t1 }
	recordDashboardActivity(db, &user)

	var sessions []DashboardSession
	db.Where("user_id = ?", user.ID).Find(&sessions)
	if len(sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(sessions))
	}
	if !sessions[0].LastActivityAt.Equal(t0) {
		t.Errorf("expected last_activity_at to stay at %v (throttled), got %v", t0, sessions[0].LastActivityAt)
	}
}

func TestRecordDashboardActivity_StartsNewSessionAfterGapExceeded(t *testing.T) {
	db := setupTestDB(t)
	org := Organization{ID: "org-rda4", Name: "RDA Org 4"}
	db.Create(&org)
	user := User{ID: "user-rda4", Email: "rda4@test.com", Name: "RDA User", OrgID: org.ID, Role: "member"}
	db.Create(&user)

	t0 := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)
	oldClock := dashboardActivityClock
	defer func() { dashboardActivityClock = oldClock }()

	dashboardActivityClock = func() time.Time { return t0 }
	recordDashboardActivity(db, &user)

	// 31 minutes later: past the 30-minute inactivity gap, so this must
	// close the first session (implicitly, by starting a new one) rather
	// than extend it.
	t1 := t0.Add(31 * time.Minute)
	dashboardActivityClock = func() time.Time { return t1 }
	recordDashboardActivity(db, &user)

	var sessions []DashboardSession
	if err := db.Where("user_id = ?", user.ID).Order("started_at asc").Find(&sessions).Error; err != nil {
		t.Fatalf("query sessions: %v", err)
	}
	if len(sessions) != 2 {
		t.Fatalf("expected 2 separate sessions, got %d", len(sessions))
	}
	if !sessions[0].StartedAt.Equal(t0) || !sessions[0].LastActivityAt.Equal(t0) {
		t.Errorf("first session should be untouched: %+v", sessions[0])
	}
	if !sessions[1].StartedAt.Equal(t1) || !sessions[1].LastActivityAt.Equal(t1) {
		t.Errorf("second session should start fresh at %v: %+v", t1, sessions[1])
	}
}

func TestResolveCookieUser_NoCookieReturnsNil(t *testing.T) {
	db := setupTestDB(t)
	req := httptest.NewRequest("GET", "/api/v1/jobs", nil)
	if user := resolveCookieUser(db, req); user != nil {
		t.Errorf("expected nil for a request with no session cookie, got %+v", user)
	}
}

func TestResolveCookieUser_ValidCookieReturnsUser(t *testing.T) {
	db := setupTestDB(t)
	org := Organization{ID: "org-rcu", Name: "RCU Org"}
	db.Create(&org)
	user := User{ID: "user-rcu", Email: "rcu@test.com", Name: "RCU User", OrgID: org.ID, Role: "member"}
	db.Create(&user)

	req := httptest.NewRequest("GET", "/api/v1/jobs", nil)
	req.AddCookie(&http.Cookie{Name: SessionCookieName, Value: CreateSessionCookieValue(user.ID)})

	got := resolveCookieUser(db, req)
	if got == nil || got.ID != user.ID {
		t.Errorf("expected resolved user %s, got %+v", user.ID, got)
	}
}
```

`dashboard_session_test.go` currently imports only `"testing"` and `"time"` (from Task 2 Step 6). Change its import block to:

```go
import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)
```

- [ ] **Step 2: Run the new tests to verify they fail**

Run: `CGO_ENABLED=0 go test ./ee/auth/... -run 'TestRecordDashboardActivity|TestResolveCookieUser' -v`
Expected: FAIL — `dashboardActivityClock`, `recordDashboardActivity`, `resolveCookieUser` undefined

- [ ] **Step 3: Implement `resolveCookieUser` and `recordDashboardActivity`**

In `ee/auth/dashboard_session.go`, change the import block and add the new code:

```go
import (
	"net/http"
	"time"

	"github.com/ibreakthecloud/kiwi/pkg/store"
	"gorm.io/gorm"
)
```

Append after the `TableName` method:

```go
const (
	// dashboardSessionGap is the inactivity gap that closes a session and
	// starts a new one on the next activity. 30 minutes matches the
	// industry-standard default (e.g. Google Analytics) for sessionizing
	// activity when there is no explicit login/logout pair to bound it.
	dashboardSessionGap = 30 * time.Minute

	// dashboardActivityWriteThrottle bounds how often a single user's
	// activity is written to the database. The dashboard SPA polls several
	// endpoints every few seconds; without this, every one of those
	// requests would trigger a write. Last-seen lagging real activity by
	// under a minute is immaterial at the granularity internal ops needs
	// this for.
	dashboardActivityWriteThrottle = 60 * time.Second
)

// dashboardActivityClock is overridden in tests to control what
// recordDashboardActivity treats as "now", so sessionization (extend vs.
// new vs. gap-close) can be tested without sleeping. Mirrors the
// githubEndpoint/githubAPIURL var-override pattern already used in
// oauth_test.go.
var dashboardActivityClock = time.Now

// resolveCookieUser resolves the session cookie on r, if present and valid,
// to the User it names. It returns nil — not an error — when there is no
// usable cookie, since the caller (AuthMiddleware / AuthFunc) falls back to
// bearer-token auth in that case.
func resolveCookieUser(db *gorm.DB, r *http.Request) *User {
	cookie, err := r.Cookie(SessionCookieName)
	if err != nil || cookie.Value == "" {
		return nil
	}
	sess, err := VerifySession(cookie.Value)
	if err != nil {
		return nil
	}
	var user User
	if err := db.First(&user, "id = ?", sess.UserID).Error; err != nil {
		return nil
	}
	return &user
}

// recordDashboardActivity extends or starts a DashboardSession for user and
// bumps User.LastSeenAt. It is called only from the cookie-authenticated
// path — API-key and bootstrap-token auth never reach it, so CLI/SDK/daemon
// traffic is never tracked as dashboard activity. This is best-effort:
// write failures are silently dropped, since recording activity must never
// fail the request it rode in on.
func recordDashboardActivity(db *gorm.DB, user *User) {
	now := dashboardActivityClock()

	var last DashboardSession
	err := db.Where("user_id = ?", user.ID).Order("started_at DESC").First(&last).Error
	if err == nil && now.Sub(last.LastActivityAt) < dashboardActivityWriteThrottle {
		return
	}

	if err == nil && now.Sub(last.LastActivityAt) <= dashboardSessionGap {
		db.Model(&DashboardSession{}).Where("id = ?", last.ID).Update("last_activity_at", now)
	} else {
		db.Create(&DashboardSession{
			ID:             store.NewDashID("dsess"),
			UserID:         user.ID,
			OrgID:          user.OrgID,
			StartedAt:      now,
			LastActivityAt: now,
		})
	}

	db.Model(&User{}).Where("id = ?", user.ID).Update("last_seen_at", now)
}
```

- [ ] **Step 4: Run the new tests to verify they pass**

Run: `CGO_ENABLED=0 go test ./ee/auth/... -run 'TestRecordDashboardActivity|TestResolveCookieUser' -v`
Expected: PASS (all 6 subtests)

- [ ] **Step 5: Run the full package test suite so far**

Run: `CGO_ENABLED=0 go test ./ee/auth/... -v`
Expected: PASS (no regressions from Tasks 1–2)

- [ ] **Step 6: Run gofmt and vet**

Run: `gofmt -l ee/auth/ && CGO_ENABLED=0 go vet ./ee/auth/...`
Expected: no output from gofmt, clean vet

- [ ] **Step 7: Commit**

```bash
git add ee/auth/dashboard_session.go ee/auth/dashboard_session_test.go
git commit -m "feat(auth): sessionize dashboard activity with a 30-minute inactivity gap"
```

---

## Task 4: Wire sign-in counting into the OAuth callback

**Files:**
- Modify: `ee/auth/oauth.go`
- Modify: `ee/auth/oauth_test.go`

**Interfaces:**
- Consumes: `User.SignInCount`, `User.LastSignInAt` (Task 1).
- Produces: nothing new consumed by later tasks — this closes out the sign-in-count half of the feature.

- [ ] **Step 1: Extend the existing OAuth flow test to assert sign-in counts**

In `ee/auth/oauth_test.go`, `TestOAuthFlow_Github` already performs two logins for the same user (the second one starting at line ~162, "Duplicate sign-in"). After the first login's user-fields assertion:

```go
	if user.Email != "test@github.local" || user.Name != "Test User" || *user.OAuthProvider != "github" || *user.OAuthSubject != "12345" {
		t.Errorf("user fields mismatch: %+v", user)
	}
```

add:

```go
	if user.SignInCount != 1 {
		t.Errorf("expected sign_in_count 1 after first login, got %d", user.SignInCount)
	}
	if user.LastSignInAt == nil {
		t.Errorf("expected last_sign_in_at to be set after first login")
	}
	firstSignInAt := user.LastSignInAt
```

Then after the duplicate-login block at the end of the test (after the existing `if len(orgs) != 1 { ... }` check for the duplicate sign-in), add:

```go
	var userAfterSecondLogin User
	if err := db.First(&userAfterSecondLogin, "id = ?", user.ID).Error; err != nil {
		t.Fatalf("reload user after second login: %v", err)
	}
	if userAfterSecondLogin.SignInCount != 2 {
		t.Errorf("expected sign_in_count 2 after second login, got %d", userAfterSecondLogin.SignInCount)
	}
	// Not a strict "after" comparison: the two logins in this test happen
	// within the same test function, potentially within the same
	// millisecond, and glebarez/sqlite's stored precision isn't guaranteed
	// finer than that — a strict .After() check would be a flaky/red
	// assertion on driver timestamp resolution, not on the behavior under
	// test. What must hold is "last_sign_in_at was re-set on the second
	// login", i.e. it did not go backwards.
	if userAfterSecondLogin.LastSignInAt == nil || userAfterSecondLogin.LastSignInAt.Before(*firstSignInAt) {
		t.Errorf("expected last_sign_in_at to be re-set (not go backwards) on second login: first=%v second=%v", firstSignInAt, userAfterSecondLogin.LastSignInAt)
	}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `CGO_ENABLED=0 go test ./ee/auth/... -run TestOAuthFlow_Github -v`
Expected: FAIL — `sign_in_count` is 0, `last_sign_in_at` is nil (field exists from Task 1, but nothing sets it yet)

- [ ] **Step 3: Record the sign-in in `handleOAuthCallback`**

In `ee/auth/oauth.go`, the user-resolution block ends and is immediately followed by the comment `// Issue session cookie`:

```go
			user.OAuthProvider = &provider
			user.OAuthSubject = &subject
			if githubLogin != nil {
				user.GitHubLogin = githubLogin
			}
			db.Save(&user)
		}
	}

	// Issue session cookie (used by the server-rendered surfaces; the SPA
	// authenticates with the bearer API key handed back below).
```

Insert a new block between the closing `}` of the resolution `if err != nil { ... }` and the `// Issue session cookie` comment, so it runs once per login regardless of which of the three resolution paths (brand-new user, existing user connecting this provider for the first time, or a returning user already matched by provider+subject) was taken:

```go
			user.OAuthProvider = &provider
			user.OAuthSubject = &subject
			if githubLogin != nil {
				user.GitHubLogin = githubLogin
			}
			db.Save(&user)
		}
	}

	// Record the sign-in. This runs once, after user is guaranteed to be the
	// row for this login — covering all three resolution paths above
	// (returning user matched by provider+subject with no code path of its
	// own, existing user now connecting this provider, and a brand-new
	// user) without duplicating the update into each branch.
	//
	// Best-effort, matching recordDashboardActivity (dashboard_session.go):
	// a failed metrics write must never be the reason a login fails, so the
	// error is dropped rather than turned into a 500. Per the Global
	// Constraints, all activity/sign-in tracking writes in this feature are
	// best-effort for the same reason.
	db.Model(&User{}).Where("id = ?", user.ID).Updates(map[string]interface{}{
		"sign_in_count":   gorm.Expr("sign_in_count + 1"),
		"last_sign_in_at": time.Now(),
	})

	// Issue session cookie (used by the server-rendered surfaces; the SPA
	// authenticates with the bearer API key handed back below).
```

No import changes are needed — `oauth.go` already imports both `"time"` and `"gorm.io/gorm"`.

- [ ] **Step 4: Run the test to verify it passes**

Run: `CGO_ENABLED=0 go test ./ee/auth/... -run TestOAuthFlow_Github -v`
Expected: PASS

- [ ] **Step 5: Run the full OAuth test file to check for regressions**

Run: `CGO_ENABLED=0 go test ./ee/auth/... -run TestOAuthFlow -v`
Expected: PASS (`TestOAuthFlow_Github` and `TestOAuthFlow_IdempotentCompanyJoin` both pass — the latter's two sign-ins should now also each bump the (new, unasserted) counters without affecting its existing org/join-request assertions)

- [ ] **Step 6: Run gofmt and vet**

Run: `gofmt -l ee/auth/ && CGO_ENABLED=0 go vet ./ee/auth/...`
Expected: no output from gofmt, clean vet

- [ ] **Step 7: Commit**

```bash
git add ee/auth/oauth.go ee/auth/oauth_test.go
git commit -m "feat(auth): record sign-in count and timestamp on every OAuth login"
```

---

## Task 5: Wire activity tracking into `AuthMiddleware` and `AuthFunc`

**Files:**
- Modify: `ee/auth/auth.go`
- Modify: `ee/auth/oauth_test.go` (extend `TestSessionAndMiddleware`)
- Modify: `ee/auth/auth_test.go` (add the API-key non-tracking guardrail test)

**Interfaces:**
- Consumes: `resolveCookieUser`, `recordDashboardActivity` (Task 3).
- Produces: nothing new consumed by later tasks. `AuthMiddleware`/`AuthFunc` signatures are unchanged — this is a behavior-only change plus a dedup of the two functions' previously-duplicated cookie-resolution blocks.

- [ ] **Step 1: Extend `TestSessionAndMiddleware` to assert activity is recorded**

In `ee/auth/oauth_test.go`, `TestSessionAndMiddleware` currently ends:

```go
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if !handlerCalled {
		t.Fatal("handler not called")
	}
}
```

Change to:

```go
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if !handlerCalled {
		t.Fatal("handler not called")
	}

	var reloaded User
	if err := db.First(&reloaded, "id = ?", user.ID).Error; err != nil {
		t.Fatalf("reload user: %v", err)
	}
	if reloaded.LastSeenAt == nil {
		t.Errorf("expected last_seen_at to be set after a cookie-authenticated request")
	}

	var sessions []DashboardSession
	if err := db.Where("user_id = ?", user.ID).Find(&sessions).Error; err != nil {
		t.Fatalf("query sessions: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("expected 1 dashboard session after a cookie-authenticated request, got %d", len(sessions))
	}
}
```

- [ ] **Step 2: Write the API-key non-tracking guardrail test**

In `ee/auth/auth_test.go`, add after `TestAuthMiddleware`:

```go
// Activity tracking must never fire for API-key authentication — only the
// browser dashboard's session cookie counts. API keys are long-lived and
// reused across CLI/SDK/daemon calls for weeks; treating that as "dashboard
// activity" would misrepresent what a human actually did, and would turn
// every CLI invocation and daemon poll into a write.
func TestAuthMiddleware_APIKeyDoesNotRecordDashboardActivity(t *testing.T) {
	db := setupTestDB(t)

	org := Organization{ID: "org-nodash", Name: "No-Dashboard Org"}
	db.Create(&org)
	user := User{ID: "user-nodash", Email: "nodash@test.com", Name: "No Dash User", OrgID: org.ID, Role: "member"}
	db.Create(&user)
	plaintext, apiKey, _ := GenerateAPIKey(user.ID, "cli-key", nil)
	db.Create(apiKey)

	testHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	middleware := AuthMiddleware(db, testHandler)

	req := httptest.NewRequest("GET", "/tasks", nil)
	req.Header.Set("Authorization", "Bearer "+plaintext)
	w := httptest.NewRecorder()
	middleware.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var reloaded User
	if err := db.First(&reloaded, "id = ?", user.ID).Error; err != nil {
		t.Fatalf("reload user: %v", err)
	}
	if reloaded.LastSeenAt != nil {
		t.Errorf("expected last_seen_at to stay nil for API-key auth, got %v", reloaded.LastSeenAt)
	}

	var sessions []DashboardSession
	if err := db.Where("user_id = ?", user.ID).Find(&sessions).Error; err != nil {
		t.Fatalf("query sessions: %v", err)
	}
	if len(sessions) != 0 {
		t.Errorf("expected 0 dashboard sessions for API-key auth, got %d", len(sessions))
	}
}
```

- [ ] **Step 3: Run both tests to verify current behavior**

Run: `CGO_ENABLED=0 go test ./ee/auth/... -run 'TestSessionAndMiddleware|TestAuthMiddleware_APIKeyDoesNotRecordDashboardActivity' -v`
Expected: `TestSessionAndMiddleware` FAILs (no session recorded yet — the cookie branch doesn't call `recordDashboardActivity` until Step 4); `TestAuthMiddleware_APIKeyDoesNotRecordDashboardActivity` PASSes already (nothing records activity yet, so the guardrail is trivially true — it will stay true after Step 4 too, since only the cookie branch changes)

- [ ] **Step 4: Refactor `AuthMiddleware` to use `resolveCookieUser` and record activity**

In `ee/auth/auth.go`, `AuthMiddleware` currently has:

```go
		token := extractBearerToken(r)
		if token == "" {
			// check for session cookie
			cookie, err := r.Cookie(SessionCookieName)
			if err == nil && cookie.Value != "" {
				sess, err := VerifySession(cookie.Value)
				if err == nil {
					var user User
					if err := db.First(&user, "id = ?", sess.UserID).Error; err == nil {
						claims := &UserClaims{
							UserID: user.ID,
							Email:  user.Email,
							Name:   user.Name,
							OrgID:  user.OrgID,
							Role:   user.Role,
						}
						next.ServeHTTP(w, r.WithContext(ContextWithClaims(r.Context(), claims)))
						return
					}
				}
			}

			http.Error(w, "Unauthorized: missing Authorization header", http.StatusUnauthorized)
			return
		}
```

Replace with:

```go
		token := extractBearerToken(r)
		if token == "" {
			if user := resolveCookieUser(db, r); user != nil {
				recordDashboardActivity(db, user)
				claims := &UserClaims{
					UserID: user.ID,
					Email:  user.Email,
					Name:   user.Name,
					OrgID:  user.OrgID,
					Role:   user.Role,
				}
				next.ServeHTTP(w, r.WithContext(ContextWithClaims(r.Context(), claims)))
				return
			}

			http.Error(w, "Unauthorized: missing Authorization header", http.StatusUnauthorized)
			return
		}
```

- [ ] **Step 5: Refactor `AuthFunc` the same way**

In `ee/auth/auth.go`, `AuthFunc` currently has:

```go
	token := extractBearerToken(r)
	if token == "" {
		cookie, err := r.Cookie(SessionCookieName)
		if err == nil && cookie.Value != "" {
			sess, err := VerifySession(cookie.Value)
			if err == nil {
				var user User
				if err := db.First(&user, "id = ?", sess.UserID).Error; err == nil {
					return &UserClaims{
						UserID: user.ID,
						Email:  user.Email,
						Name:   user.Name,
						OrgID:  user.OrgID,
						Role:   user.Role,
					}, nil
				}
			}
		}
		return nil, fmt.Errorf("missing Authorization header")
	}
```

Replace with:

```go
	token := extractBearerToken(r)
	if token == "" {
		if user := resolveCookieUser(db, r); user != nil {
			recordDashboardActivity(db, user)
			return &UserClaims{
				UserID: user.ID,
				Email:  user.Email,
				Name:   user.Name,
				OrgID:  user.OrgID,
				Role:   user.Role,
			}, nil
		}
		return nil, fmt.Errorf("missing Authorization header")
	}
```

- [ ] **Step 6: Run both tests to verify they pass**

Run: `CGO_ENABLED=0 go test ./ee/auth/... -run 'TestSessionAndMiddleware|TestAuthMiddleware_APIKeyDoesNotRecordDashboardActivity' -v`
Expected: PASS

- [ ] **Step 7: Run the full `ee/auth` suite to check for regressions**

Run: `CGO_ENABLED=0 go test ./ee/auth/... -v`
Expected: PASS — in particular `TestGenerateAndValidateAPIKey`, `TestBootstrapAdminToken`, and `TestAuthMiddleware` (the pre-existing API-key test) must be unaffected, since none of them authenticate via cookie.

- [ ] **Step 8: Run gofmt and vet**

Run: `gofmt -l ee/auth/ && CGO_ENABLED=0 go vet ./ee/auth/...`
Expected: no output from gofmt, clean vet

- [ ] **Step 9: Commit**

```bash
git add ee/auth/auth.go ee/auth/oauth_test.go ee/auth/auth_test.go
git commit -m "feat(auth): record dashboard activity for cookie-authenticated requests"
```

---

## Task 6: Admin endpoint for session history

**Files:**
- Modify: `ee/auth/admin.go`
- Modify: `ee/auth/admin_test.go`

**Interfaces:**
- Consumes: `DashboardSession` (Task 2), `authorizeOrgAccess` (existing, `ee/auth/admin.go`).
- Produces: `GET /admin/orgs/{orgID}/users/{userID}/sessions`, and the `dashboardSessionResponse` type it serializes — terminal for this plan; nothing later depends on either.

- [ ] **Step 1: Write the endpoint test first**

In `ee/auth/admin_test.go`, add after `TestAPIKeyHandlers_RejectCrossOrgUser`:

```go
func TestListSessions(t *testing.T) {
	db := setupTestDB(t)
	mux := http.NewServeMux()
	AdminRouter(db, mux)

	orgA := Organization{ID: "org-sess-a", Name: "Sessions Org A"}
	orgB := Organization{ID: "org-sess-b", Name: "Sessions Org B"}
	db.Create(&orgA)
	db.Create(&orgB)
	userInA := User{ID: "user-sess-a", Email: "a@example.com", Name: "User A", OrgID: "org-sess-a", Role: "member"}
	userInB := User{ID: "user-sess-b", Email: "b@example.com", Name: "User B", OrgID: "org-sess-b", Role: "member"}
	db.Create(&userInA)
	db.Create(&userInB)

	claims := &UserClaims{UserID: "system"}
	ctx := ContextWithClaims(context.Background(), claims)

	// Empty: no sessions recorded yet.
	reqEmpty := httptest.NewRequest(http.MethodGet, "/admin/orgs/org-sess-a/users/user-sess-a/sessions", nil).WithContext(ctx)
	wEmpty := httptest.NewRecorder()
	mux.ServeHTTP(wEmpty, reqEmpty)
	if wEmpty.Code != http.StatusOK {
		t.Fatalf("expected 200 for empty session list, got %d: %s", wEmpty.Code, wEmpty.Body.String())
	}
	var emptyResult []dashboardSessionResponse
	if err := json.Unmarshal(wEmpty.Body.Bytes(), &emptyResult); err != nil {
		t.Fatalf("decode empty response: %v", err)
	}
	if len(emptyResult) != 0 {
		t.Errorf("expected 0 sessions, got %d", len(emptyResult))
	}

	// Populated: two sessions, must come back newest first, each carrying
	// its computed duration_seconds.
	olderStart := time.Now().Add(-2 * time.Hour)
	olderEnd := olderStart.Add(10 * time.Minute)
	older := DashboardSession{ID: "dsess_older", UserID: userInA.ID, OrgID: orgA.ID,
		StartedAt: olderStart, LastActivityAt: olderEnd}
	newerStart := time.Now()
	newer := DashboardSession{ID: "dsess_newer", UserID: userInA.ID, OrgID: orgA.ID,
		StartedAt: newerStart, LastActivityAt: newerStart}
	db.Create(&older)
	db.Create(&newer)

	reqList := httptest.NewRequest(http.MethodGet, "/admin/orgs/org-sess-a/users/user-sess-a/sessions", nil).WithContext(ctx)
	wList := httptest.NewRecorder()
	mux.ServeHTTP(wList, reqList)
	if wList.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", wList.Code, wList.Body.String())
	}
	var result []dashboardSessionResponse
	if err := json.Unmarshal(wList.Body.Bytes(), &result); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("expected 2 sessions, got %d", len(result))
	}
	if result[0].ID != "dsess_newer" || result[1].ID != "dsess_older" {
		t.Errorf("expected newest-first order, got %s then %s", result[0].ID, result[1].ID)
	}
	// The still-open session (last activity == start) has ~zero duration;
	// the closed one ran 10 minutes.
	if result[0].DurationSeconds < 0 || result[0].DurationSeconds > 1 {
		t.Errorf("expected the newer session's duration to be ~0s, got %v", result[0].DurationSeconds)
	}
	if result[1].DurationSeconds < 599 || result[1].DurationSeconds > 601 {
		t.Errorf("expected the older session's duration to be ~600s, got %v", result[1].DurationSeconds)
	}

	// Cross-org: user-sess-b belongs to org-sess-b, not org-sess-a.
	reqCross := httptest.NewRequest(http.MethodGet, "/admin/orgs/org-sess-a/users/user-sess-b/sessions", nil).WithContext(ctx)
	wCross := httptest.NewRecorder()
	mux.ServeHTTP(wCross, reqCross)
	if wCross.Code != http.StatusNotFound {
		t.Errorf("expected 404 for cross-org user, got %d: %s", wCross.Code, wCross.Body.String())
	}
}
```

`admin_test.go` already imports `"bytes"`, `"context"`, `"encoding/json"`, `"fmt"`, `"net/http"`, `"net/http/httptest"`, `"os"`, `"testing"`, plus `ee/audit` and `pkg/store` — only `"time"` is missing. Add it to the stdlib group:

```go
import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/ibreakthecloud/kiwi/ee/audit"
	"github.com/ibreakthecloud/kiwi/pkg/store"
)
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `CGO_ENABLED=0 go test ./ee/auth/... -run TestListSessions -v`
Expected: FAIL — 404 on all three requests (no route matches `/sessions` yet)

- [ ] **Step 3: Add the router case**

In `ee/auth/admin.go`, the `/admin/orgs/` dispatcher has this case (around the existing `.../users/{userID}/keys` GET/POST case):

```go
		case len(parts) == 4 && parts[1] == "users" && parts[3] == "keys":
			orgID := parts[0]
			userID := parts[2]
			if !authorizeOrgAccess(r, orgID) {
				http.Error(w, "Forbidden: admin access required", http.StatusForbidden)
				return
			}
			switch r.Method {
			case http.MethodPost:
				handleCreateAPIKey(db, w, r, orgID, userID)
			case http.MethodGet:
				handleListAPIKeys(db, w, r, orgID, userID)
			default:
				http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			}
```

Add a new case immediately after it (before the `case len(parts) == 5 && parts[1] == "users" && parts[3] == "keys":` case):

```go
		case len(parts) == 4 && parts[1] == "users" && parts[3] == "sessions":
			orgID := parts[0]
			userID := parts[2]
			if !authorizeOrgAccess(r, orgID) {
				http.Error(w, "Forbidden: admin access required", http.StatusForbidden)
				return
			}
			if r.Method != http.MethodGet {
				http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
				return
			}
			handleListSessions(db, w, r, orgID, userID)
```

- [ ] **Step 4: Add the handler**

In `ee/auth/admin.go`, right after `handleListAPIKeys` (before `handleRevokeAPIKey`), add:

```go
// dashboardSessionResponse is what handleListSessions returns: the stored
// DashboardSession fields plus the derived length ops actually asked for
// ("how long was their session"), so the API answers that directly instead
// of making every caller subtract LastActivityAt - StartedAt itself.
type dashboardSessionResponse struct {
	ID              string    `json:"id"`
	UserID          string    `json:"user_id"`
	OrgID           string    `json:"org_id"`
	StartedAt       time.Time `json:"started_at"`
	LastActivityAt  time.Time `json:"last_activity_at"`
	DurationSeconds float64   `json:"duration_seconds"`
}

// handleListSessions returns a user's most recent dashboard sessions,
// newest first, so ops can see session-length history rather than a single
// last-seen timestamp.
func handleListSessions(db *gorm.DB, w http.ResponseWriter, r *http.Request, orgID, userID string) {
	var user User
	if err := db.First(&user, "id = ?", userID).Error; err != nil || user.OrgID != orgID {
		http.Error(w, "User not found", http.StatusNotFound)
		return
	}
	var sessions []DashboardSession
	if err := db.Where("user_id = ?", userID).Order("started_at desc").Limit(20).Find(&sessions).Error; err != nil {
		http.Error(w, "Failed to list sessions", http.StatusInternalServerError)
		return
	}
	resp := make([]dashboardSessionResponse, len(sessions))
	for i, s := range sessions {
		resp[i] = dashboardSessionResponse{
			ID:              s.ID,
			UserID:          s.UserID,
			OrgID:           s.OrgID,
			StartedAt:       s.StartedAt,
			LastActivityAt:  s.LastActivityAt,
			DurationSeconds: s.LastActivityAt.Sub(s.StartedAt).Seconds(),
		}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}
```

- [ ] **Step 5: Run the test to verify it passes**

Run: `CGO_ENABLED=0 go test ./ee/auth/... -run TestListSessions -v`
Expected: PASS

- [ ] **Step 6: Run the full `ee/auth` suite**

Run: `CGO_ENABLED=0 go test ./ee/auth/... -v`
Expected: PASS, all tests including every prior task's

- [ ] **Step 7: Run gofmt and vet**

Run: `gofmt -l ee/auth/ && CGO_ENABLED=0 go vet ./ee/auth/...`
Expected: no output from gofmt, clean vet

- [ ] **Step 8: Commit**

```bash
git add ee/auth/admin.go ee/auth/admin_test.go
git commit -m "feat(auth): expose dashboard session history via admin API"
```

---

## Task 7: Full-repo verification

**Files:** none (verification only)

**Interfaces:** none

- [ ] **Step 1: Run the full mandated pre-commit checks from CLAUDE.md**

```bash
gofmt -l cmd/ pkg/ ee/
CGO_ENABLED=0 go vet ./...
CGO_ENABLED=0 go test ./pkg/...
CGO_ENABLED=0 go build ./...
```

Expected: `gofmt` prints nothing; `go vet` clean; `go test ./pkg/...` passes (this feature touches only `ee/auth` and `migrations/`, so this is a no-regression check, not new coverage); `go build ./...` succeeds.

- [ ] **Step 2: Run the full `ee` suite, since that's where this feature's own tests live**

```bash
CGO_ENABLED=0 go test ./ee/...
```

Expected: PASS.

- [ ] **Step 3: If anything fails, fix and re-run before proceeding — do not commit past a red run**

No commit for this task — it's a verification gate, not a new deliverable. If Steps 1–2 are clean, the branch is ready to open as a PR (per `CLAUDE.md` §3: since this touches only `ee/auth` and `migrations/`, not `README.md`, the PR needs the `skip-readme-check` label).
