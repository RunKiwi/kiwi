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
