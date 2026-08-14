// SPDX-License-Identifier: LicenseRef-Kiwi-BSL-1.1
// Copyright (c) 2026 RunKiwi. Licensed under the Business Source License 1.1.
// See ee/LICENSE. This is Control Plane code and is NOT Apache-2.0.

package auth

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
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

// A transient DB error on the initial session lookup (a connection blip, a
// timeout — anything that isn't "no row found") must abort the activity
// recording entirely, not be treated as "no prior session exists" and
// fabricate a new session row. This uses a file-backed sqlite DB (not the
// usual in-memory one) so the underlying *sql.DB can be closed to force a
// real, deterministic non-ErrRecordNotFound error on the next query, then
// reopened against the same file to inspect what was (and wasn't) written —
// without pulling in a mocking library.
func TestRecordDashboardActivity_TransientDBErrorDoesNotFabricateSession(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := InitAuthDB(db); err != nil {
		t.Fatalf("migrate auth db: %v", err)
	}

	org := Organization{ID: "org-rda5", Name: "RDA Org 5"}
	db.Create(&org)
	user := User{ID: "user-rda5", Email: "rda5@test.com", Name: "RDA User", OrgID: org.ID, Role: "member"}
	db.Create(&user)

	oldClock := dashboardActivityClock
	defer func() { dashboardActivityClock = oldClock }()

	t0 := time.Date(2026, 8, 14, 12, 0, 0, 0, time.UTC)
	dashboardActivityClock = func() time.Time { return t0 }
	recordDashboardActivity(db, &user)

	// Force every subsequent query on this *gorm.DB to fail with a real
	// connection error (not gorm.ErrRecordNotFound) by closing the
	// underlying *sql.DB out from under it.
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get sql.DB: %v", err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatalf("close sql.DB: %v", err)
	}

	// 31 minutes later: past the inactivity gap, so absent the fix this
	// would take the "start a new session" branch on a fabricated "not
	// found". recordDashboardActivity must not panic, and must not write
	// anything, since the lookup itself failed for a non-not-found reason.
	t1 := t0.Add(31 * time.Minute)
	dashboardActivityClock = func() time.Time { return t1 }
	recordDashboardActivity(db, &user)

	// Reopen a fresh connection against the same file to inspect state
	// without going through the now-closed *gorm.DB.
	verifyDB, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	if err != nil {
		t.Fatalf("reopen db: %v", err)
	}

	var sessions []DashboardSession
	if err := verifyDB.Where("user_id = ?", user.ID).Find(&sessions).Error; err != nil {
		t.Fatalf("query sessions: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("expected the transient error to prevent a second session from being fabricated, got %d sessions", len(sessions))
	}
	if !sessions[0].LastActivityAt.Equal(t0) {
		t.Errorf("expected last_activity_at to remain at %v (write dropped on transient error), got %v", t0, sessions[0].LastActivityAt)
	}

	var reloaded User
	if err := verifyDB.First(&reloaded, "id = ?", user.ID).Error; err != nil {
		t.Fatalf("reload user: %v", err)
	}
	if reloaded.LastSeenAt == nil || !reloaded.LastSeenAt.Equal(t0) {
		t.Errorf("expected last_seen_at to remain at %v (write dropped on transient error), got %v", t0, reloaded.LastSeenAt)
	}
}
