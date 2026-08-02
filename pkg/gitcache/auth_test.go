package gitcache

import (
	"context"
	"encoding/base64"
	"path/filepath"
	"strings"
	"testing"
)

// The header must be scoped to the remote's host. An unscoped
// http.extraHeader is attached to every request git makes, including one
// following a redirect to another host — which hands the credential to a third
// party. Scoping is the difference between a working private clone and a
// credential leak, so it is asserted rather than assumed.
func TestAuthArgsScopesTheHeaderToTheRemoteHost(t *testing.T) {
	args := authArgs("https://github.com/acme/widgets", "tok-123")
	if len(args) != 2 || args[0] != "-c" {
		t.Fatalf("expected a single -c pair, got %v", args)
	}

	const wantScope = "http.https://github.com/.extraHeader="
	if !strings.HasPrefix(args[1], wantScope) {
		t.Errorf("header not scoped to the remote host:\n got %q\nwant prefix %q", args[1], wantScope)
	}
	if strings.HasPrefix(args[1], "http.extraHeader=") {
		t.Error("header is unscoped; it would be sent to any host git is redirected to")
	}

	wantBasic := base64.StdEncoding.EncodeToString([]byte("x-access-token:tok-123"))
	if !strings.HasSuffix(args[1], "Authorization: Basic "+wantBasic) {
		t.Errorf("credential not encoded as expected: %q", args[1])
	}
}

// The token must never reach the remote URL, because `clone` persists its URL
// argument into the bare repo's config. A -c value lives only for the process.
func TestAuthArgsDoesNotTouchTheRemoteURL(t *testing.T) {
	args := authArgs("https://github.com/acme/widgets", "tok-123")
	for _, a := range args {
		if strings.Contains(a, "tok-123@") || strings.Contains(a, "x-access-token:tok-123@") {
			t.Errorf("token embedded in a URL-like position: %q", a)
		}
	}
}

func TestAuthArgsNoOpsWhereAHeaderCannotApply(t *testing.T) {
	tests := []struct {
		name, url, token string
	}{
		{"no token", "https://github.com/acme/widgets", ""},
		{"ssh remote", "git@github.com:acme/widgets.git", "tok"},
		{"local path", "/tmp/some/repo", "tok"},
		{"empty url", "", "tok"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := authArgs(tc.url, tc.token); got != nil {
				t.Errorf("expected no auth args, got %v", got)
			}
		})
	}
}

// git echoes the failing command back on error, and that command carries the
// -c value. Both the raw token and its base64 form have to be scrubbed or a
// clone failure prints the credential into the task log.
func TestScrubRemovesBothTokenForms(t *testing.T) {
	const tok = "ghp_supersecret"
	basic := base64.StdEncoding.EncodeToString([]byte("x-access-token:" + tok))

	in := "git -c http.https://github.com/.extraHeader=Authorization: Basic " + basic +
		" clone failed, token was " + tok
	got := scrub(in, tok)

	if strings.Contains(got, tok) {
		t.Error("raw token survived scrubbing")
	}
	if strings.Contains(got, basic) {
		t.Error("base64 credential survived scrubbing")
	}
	if !strings.Contains(got, "***") {
		t.Errorf("expected a redaction marker, got %q", got)
	}
}

// Passing a token for a remote that needs none must not change behaviour. The
// local-path case is what every other test in this package exercises, so a
// regression here would break the whole suite's premise.
func TestWithTokenDoesNotBreakAnUnauthenticatedRemote(t *testing.T) {
	src := newTestRepo(t)
	cache, cleanup := setupTestCache(t)
	defer cleanup()

	wt := filepath.Join(cache.baseDir, "wt")
	if err := cache.GetWorktree(context.Background(), src, "main", wt, WithToken("irrelevant")); err != nil {
		t.Fatalf("clone of a local repo with a token set: %v", err)
	}
	if _, err := filepath.Glob(filepath.Join(wt, "a.txt")); err != nil {
		t.Fatalf("worktree missing expected content: %v", err)
	}
}

// A nil Option must not panic; the daemon builds the slice unconditionally.
func TestApplyToleratesNilOptions(t *testing.T) {
	if got := apply([]Option{nil, WithToken("t"), nil}).token; got != "t" {
		t.Errorf("token = %q, want %q", got, "t")
	}
}
