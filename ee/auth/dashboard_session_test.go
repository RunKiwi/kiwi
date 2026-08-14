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
