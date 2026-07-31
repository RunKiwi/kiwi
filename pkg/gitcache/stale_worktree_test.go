package gitcache

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// newTestRepo builds a tiny git repository and returns its path.
func newTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q", "-b", "main")
	run("config", "user.email", "test@example.com")
	run("config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-qm", "init")
	return dir
}

// The production failure, reproduced exactly:
//
//	fatal: '<path>' is a missing but locked worktree;
//	use 'add -f -f' to override, or 'unlock' and 'prune' or 'remove' to clear
//
// `git worktree add` writes `locked` containing "initializing" into the admin
// directory before checking anything out, and removes it on success. A process
// killed in that window — a stopped daemon container, a task timeout, a revoked
// lease — leaves the lock behind. The directory is then deleted on the next
// attempt, so the entry is missing AND locked, which `git worktree prune`
// refuses to touch. Every later run for that task fails.
//
// This was survivable while the cache was ephemeral. The free-tier cache is a
// persistent host bind mount, so one interrupted task poisons that path
// permanently: the task can never run again on that daemon.
func TestGetWorktree_RecoversFromAnInterruptedPreviousAdd(t *testing.T) {
	src := newTestRepo(t)
	cache, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	wt := filepath.Join(t.TempDir(), "worktree")

	// A first, successful provision.
	if err := cache.GetWorktree(ctx, src, "main", wt); err != nil {
		t.Fatalf("initial GetWorktree: %v", err)
	}

	// Simulate the interrupted add: lock the registration, then remove the
	// directory, which is exactly the state a killed `worktree add` leaves.
	bare := cache.repoPath(src)
	if out, err := exec.Command("git", "-C", bare, "worktree", "lock", wt).CombinedOutput(); err != nil {
		t.Fatalf("lock worktree: %v\n%s", err, out)
	}
	if err := os.RemoveAll(wt); err != nil {
		t.Fatal(err)
	}

	// The next run must recover on its own. Before this fix it failed here,
	// permanently, for that task path.
	if err := cache.GetWorktree(ctx, src, "main", wt); err != nil {
		t.Fatalf("GetWorktree did not recover from a stale locked worktree: %v", err)
	}
	if _, err := os.Stat(filepath.Join(wt, "a.txt")); err != nil {
		t.Errorf("worktree was not actually provisioned: %v", err)
	}
}

// The same failure on the job-branch path, which has its own provisioning
// routine and would otherwise keep the bug.
func TestGetJobWorktree_RecoversFromAnInterruptedPreviousAdd(t *testing.T) {
	src := newTestRepo(t)
	cache, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	wt := filepath.Join(t.TempDir(), "worktree")

	if err := cache.GetJobWorktree(ctx, src, "main", "kiwi/job_1", wt); err != nil {
		t.Fatalf("initial GetJobWorktree: %v", err)
	}

	bare := cache.repoPath(src)
	if out, err := exec.Command("git", "-C", bare, "worktree", "lock", wt).CombinedOutput(); err != nil {
		t.Fatalf("lock worktree: %v\n%s", err, out)
	}
	if err := os.RemoveAll(wt); err != nil {
		t.Fatal(err)
	}

	if err := cache.GetJobWorktree(ctx, src, "main", "kiwi/job_1", wt); err != nil {
		t.Fatalf("GetJobWorktree did not recover from a stale locked worktree: %v", err)
	}
}

// The safety property that distinguishes this from `worktree add -f -f`.
//
// A locked worktree whose directory still EXISTS may be one a running task is
// working in. Unlocking or overriding it would let two runs share a directory,
// and delivery runs `git add -A` — so one task would commit the other's
// half-written files. Only registrations whose directory is gone are cleared.
func TestClearStaleWorktrees_LeavesLivePresentWorktreesLocked(t *testing.T) {
	src := newTestRepo(t)
	cache, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	live := filepath.Join(t.TempDir(), "live")
	if err := cache.GetWorktree(ctx, src, "main", live); err != nil {
		t.Fatal(err)
	}

	bare := cache.repoPath(src)
	if out, err := exec.Command("git", "-C", bare, "worktree", "lock", live).CombinedOutput(); err != nil {
		t.Fatalf("lock worktree: %v\n%s", err, out)
	}

	// The directory is present, so this must not be touched.
	clearStaleWorktrees(ctx, bare)

	out, err := exec.Command("git", "-C", bare, "worktree", "list", "--porcelain").CombinedOutput()
	if err != nil {
		t.Fatalf("worktree list: %v\n%s", err, out)
	}
	if !contains(string(out), "locked") {
		t.Error("a locked worktree that still exists on disk was unlocked; a running task could be sharing it")
	}
	if _, err := os.Stat(filepath.Join(live, "a.txt")); err != nil {
		t.Errorf("the live worktree was disturbed: %v", err)
	}
}

// Cleanup must be safe to run against a repository with nothing stale, which is
// the common case on every single provision.
func TestClearStaleWorktrees_NoopOnAHealthyRepo(t *testing.T) {
	src := newTestRepo(t)
	cache, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	wt := filepath.Join(t.TempDir(), "worktree")

	if err := cache.GetWorktree(ctx, src, "main", wt); err != nil {
		t.Fatal(err)
	}
	clearStaleWorktrees(ctx, cache.repoPath(src))

	if _, err := os.Stat(filepath.Join(wt, "a.txt")); err != nil {
		t.Errorf("a healthy worktree was removed by cleanup: %v", err)
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (func() bool {
		for i := 0; i+len(needle) <= len(haystack); i++ {
			if haystack[i:i+len(needle)] == needle {
				return true
			}
		}
		return false
	})()
}
