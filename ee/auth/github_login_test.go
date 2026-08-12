// SPDX-License-Identifier: LicenseRef-Kiwi-BSL-1.1
// Copyright (c) 2026 RunKiwi. Licensed under the Business Source License 1.1.
// See ee/LICENSE. This is Control Plane code and is NOT Apache-2.0.

package auth

import (
	"testing"
	"time"
)

// The GitHub username is worth keeping, and was not kept.
//
// The callback fetched `login` and used it only as a fallback for the display
// name when the profile had none, then dropped it. What survived was
// OAuthSubject — GitHub's numeric id, which is stable and unguessable and
// answers no question anyone asks. "Which GitHub user is this?" needed an API
// call, and support could not match a person to an account at all.
func TestUserStoresGitHubLogin(t *testing.T) {
	db := setupTestDB(t)

	provider, subject := "github", "12345"
	login := "octocat"
	u := User{
		ID: "u_1", Email: "octocat@example.com", Name: "The Octocat",
		OrgID: "org_1", Role: "admin",
		OAuthProvider: &provider, OAuthSubject: &subject,
		GitHubLogin: &login,
		CreatedAt:   time.Now(),
	}
	if err := db.Create(&u).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}

	var got User
	if err := db.First(&got, "id = ?", "u_1").Error; err != nil {
		t.Fatal(err)
	}
	if got.GitHubLogin == nil || *got.GitHubLogin != "octocat" {
		t.Errorf("GitHubLogin = %v, want octocat", got.GitHubLogin)
	}
}

// Nullable, not empty-string. A Google user has no GitHub username, and
// recording "" would make "no account" and "an account we failed to capture"
// the same value — including for every user who signed up before this column
// existed and cannot be backfilled.
func TestGitHubLoginIsNilForNonGitHubUsers(t *testing.T) {
	db := setupTestDB(t)

	provider, subject := "google", "g-1"
	u := User{
		ID: "u_2", Email: "someone@gmail.com", Name: "Someone",
		OrgID: "org_1", Role: "member",
		OAuthProvider: &provider, OAuthSubject: &subject,
		CreatedAt: time.Now(),
	}
	if err := db.Create(&u).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}

	var got User
	if err := db.First(&got, "id = ?", "u_2").Error; err != nil {
		t.Fatal(err)
	}
	if got.GitHubLogin != nil {
		t.Errorf("GitHubLogin = %v, want nil for a Google user", *got.GitHubLogin)
	}
}

// Two GitHub users must be distinguishable, and a login is not a key: GitHub
// lets an account be renamed and the freed name re-registered, so the column
// must not carry a unique constraint that a rename could violate.
func TestGitHubLoginIsNotUnique(t *testing.T) {
	db := setupTestDB(t)

	provider := "github"
	for i, id := range []string{"u_3", "u_4"} {
		subject := string(rune('a' + i))
		login := "renamed-account"
		u := User{
			ID: id, Email: id + "@example.com", Name: id,
			OrgID: "org_1", Role: "member",
			OAuthProvider: &provider, OAuthSubject: &subject,
			GitHubLogin: &login,
			CreatedAt:   time.Now(),
		}
		if err := db.Create(&u).Error; err != nil {
			t.Fatalf("create %s: a reused GitHub login must not collide: %v", id, err)
		}
	}
}
