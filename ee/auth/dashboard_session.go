// SPDX-License-Identifier: LicenseRef-Kiwi-BSL-1.1
// Copyright (c) 2026 RunKiwi. Licensed under the Business Source License 1.1.
// See ee/LICENSE. This is Control Plane code and is NOT Apache-2.0.

package auth

import (
	"errors"
	"net/http"
	"time"

	"github.com/ibreakthecloud/kiwi/pkg/store"
	"gorm.io/gorm"
)

// DashboardSession is a sessionized span of browser-dashboard activity for a
// user, derived from cookie-authenticated requests and — the common case in
// practice, since the SPA authenticates with a bearer token — from
// bearer-token requests whose resolved API key is labeled
// WebSessionAPIKeyLabel. It is deliberately not
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
	OrgID          string    `json:"org_id" gorm:"not null"`
	StartedAt      time.Time `json:"started_at" gorm:"not null"`
	LastActivityAt time.Time `json:"last_activity_at" gorm:"not null"`
}

// TableName overrides the default GORM table name.
func (DashboardSession) TableName() string { return "dashboard_sessions" }

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
// bumps User.LastSeenAt. It is called from the cookie-fallback path in both
// AuthMiddleware and AuthFunc, and from the bearer-token path in both when
// the resolved API key's Label equals WebSessionAPIKeyLabel — that label is
// the actual "this is the browser dashboard" signal, since the SPA
// authenticates every request with that key as a bearer token, not the
// session cookie. Bootstrap-token auth and any other API key label never
// reach it, so CLI/SDK/daemon traffic is never tracked as dashboard
// activity. This is best-effort: write failures are silently dropped, since
// recording activity must never fail the request it rode in on.
func recordDashboardActivity(db *gorm.DB, user *User) {
	now := dashboardActivityClock()

	var last DashboardSession
	err := db.Where("user_id = ?", user.ID).Order("started_at DESC").First(&last).Error
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		// A real DB error, not "no prior session" — best-effort means
		// dropping this update rather than risking a transient read
		// failure fabricating a duplicate session (see the ErrRecordNotFound
		// distinction below).
		return
	}

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
