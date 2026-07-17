package gitcache

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
)

// makeLocalRepo creates a real local git repository with one commit on main and
// returns a file:// URL for it. Using local repos keeps the eviction tests off
// the network so they are fast and deterministic in CI.
func makeLocalRepo(t *testing.T, name string) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte(name), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	run("add", ".")
	run("commit", "-q", "-m", "init")
	return "file://" + dir
}

// countBareRepos counts cached bare repos on disk (each has a HEAD at its root).
func countBareRepos(t *testing.T, baseDir string) int {
	t.Helper()
	entries, err := os.ReadDir(baseDir)
	if err != nil {
		t.Fatalf("read cache dir: %v", err)
	}
	n := 0
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if _, err := os.Stat(filepath.Join(baseDir, e.Name(), "HEAD")); err == nil {
			n++
		}
	}
	return n
}

// getAndRelease provisions a worktree then immediately removes it, so the repo
// is cached but has no live worktree (active == 0, thus evictable).
func getAndRelease(t *testing.T, c *Cache, repoURL string) {
	t.Helper()
	ctx := context.Background()
	wt := filepath.Join(t.TempDir(), "wt")
	if err := c.GetWorktree(ctx, repoURL, "main", wt); err != nil {
		t.Fatalf("GetWorktree(%s): %v", repoURL, err)
	}
	if err := c.RemoveWorktree(ctx, repoURL, wt); err != nil {
		t.Fatalf("RemoveWorktree(%s): %v", repoURL, err)
	}
}

func TestCache_EvictsWhenOverLimit(t *testing.T) {
	base := t.TempDir()
	c, err := NewWithLimit(base, 2)
	if err != nil {
		t.Fatalf("NewWithLimit: %v", err)
	}

	r1 := makeLocalRepo(t, "r1")
	r2 := makeLocalRepo(t, "r2")
	r3 := makeLocalRepo(t, "r3")

	// Fill to the bound: two repos cached.
	getAndRelease(t, c, r1)
	getAndRelease(t, c, r2)
	if got := countBareRepos(t, base); got != 2 {
		t.Fatalf("after 2 repos, cached = %d, want 2", got)
	}

	// A third clone must evict one, keeping the count at the bound.
	getAndRelease(t, c, r3)
	if got := countBareRepos(t, base); got != 2 {
		t.Errorf("after 3rd repo, cached = %d, want 2 (eviction should hold the bound)", got)
	}
}

func TestCache_EvictsLeastFrequentlyUsed(t *testing.T) {
	base := t.TempDir()
	c, err := NewWithLimit(base, 2)
	if err != nil {
		t.Fatalf("NewWithLimit: %v", err)
	}

	r1 := makeLocalRepo(t, "r1")
	r2 := makeLocalRepo(t, "r2")
	r3 := makeLocalRepo(t, "r3")

	// r1 used three times, r2 once. r2 is the least-frequently-used.
	getAndRelease(t, c, r1)
	getAndRelease(t, c, r1)
	getAndRelease(t, c, r1)
	getAndRelease(t, c, r2)

	// Adding r3 should evict r2 (LFU), not r1.
	getAndRelease(t, c, r3)

	if _, err := os.Stat(c.repoPath(r2)); !os.IsNotExist(err) {
		t.Errorf("r2 (least-frequently-used) should have been evicted, but its dir still exists")
	}
	if _, err := os.Stat(c.repoPath(r1)); err != nil {
		t.Errorf("r1 (most-frequently-used) should have been kept, but is gone: %v", err)
	}
}

func TestCache_DoesNotEvictRepoWithLiveWorktree(t *testing.T) {
	base := t.TempDir()
	c, err := NewWithLimit(base, 1)
	if err != nil {
		t.Fatalf("NewWithLimit: %v", err)
	}
	ctx := context.Background()

	r1 := makeLocalRepo(t, "r1")
	r2 := makeLocalRepo(t, "r2")

	// Hold a live worktree on r1 (do not release it): active == 1.
	wt1 := filepath.Join(t.TempDir(), "wt1")
	if err := c.GetWorktree(ctx, r1, "main", wt1); err != nil {
		t.Fatalf("GetWorktree r1: %v", err)
	}

	// With the bound at 1, adding r2 wants to evict something — but the only
	// candidate (r1) has a live worktree and must not be evicted. Both survive;
	// the bound is deliberately exceeded rather than break a running task.
	getAndRelease(t, c, r2)

	if _, err := os.Stat(c.repoPath(r1)); err != nil {
		t.Errorf("r1 has a live worktree and must not be evicted, but it is gone: %v", err)
	}
	if _, err := os.Stat(c.repoPath(r2)); err != nil {
		t.Errorf("r2 should be present: %v", err)
	}

	// After releasing r1's worktree, it becomes evictable again.
	if err := c.RemoveWorktree(ctx, r1, wt1); err != nil {
		t.Fatalf("RemoveWorktree r1: %v", err)
	}
	r3 := makeLocalRepo(t, "r3")
	getAndRelease(t, c, r3)
	// Now something should have been evicted to get back toward the bound.
	if got := countBareRepos(t, base); got > 2 {
		t.Errorf("after releasing the live worktree, cached = %d; eviction should have resumed", got)
	}
}

func TestCache_ConcurrentGetWithEviction(t *testing.T) {
	base := t.TempDir()
	c, err := NewWithLimit(base, 3)
	if err != nil {
		t.Fatalf("NewWithLimit: %v", err)
	}

	// Pre-make several distinct repos so many goroutines drive clone+evict
	// churn against a small bound at once — this is the path TryLock guards.
	const n = 8
	repos := make([]string, n)
	for i := range repos {
		repos[i] = makeLocalRepo(t, string(rune('a'+i)))
	}

	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(url string) {
			defer wg.Done()
			getAndRelease(t, c, url)
		}(repos[i])
	}
	wg.Wait() // must return — a deadlock here would hang the test (the TryLock guard)

	// The contract is eventual convergence, not an instantaneous cap: eviction
	// cannot remove a repo another goroutine is actively cloning, so the burst
	// may transiently exceed the bound. Once churn settles, the next access must
	// bring the cache back to the bound.
	getAndRelease(t, c, makeLocalRepo(t, "z"))
	if got := countBareRepos(t, base); got != 3 {
		t.Errorf("cached = %d after churn settled; want convergence to bound 3", got)
	}
}

func TestCache_UnboundedNeverEvicts(t *testing.T) {
	base := t.TempDir()
	c, err := NewWithLimit(base, 0) // 0 = unbounded
	if err != nil {
		t.Fatalf("NewWithLimit: %v", err)
	}
	for _, name := range []string{"a", "b", "c", "d"} {
		getAndRelease(t, c, makeLocalRepo(t, name))
	}
	if got := countBareRepos(t, base); got != 4 {
		t.Errorf("unbounded cache = %d repos, want 4 (no eviction)", got)
	}
}

func TestCache_EnforcesBoundAcrossRestart(t *testing.T) {
	base := t.TempDir()

	// First process: fill two repos into the cache.
	c1, err := NewWithLimit(base, 2)
	if err != nil {
		t.Fatalf("NewWithLimit: %v", err)
	}
	getAndRelease(t, c1, makeLocalRepo(t, "r1"))
	getAndRelease(t, c1, makeLocalRepo(t, "r2"))
	if got := countBareRepos(t, base); got != 2 {
		t.Fatalf("pre-restart cached = %d, want 2", got)
	}

	// Restart: a fresh Cache over the same dir must see the existing repos and
	// keep honoring the bound rather than growing past it.
	c2, err := NewWithLimit(base, 2)
	if err != nil {
		t.Fatalf("re-open cache: %v", err)
	}
	getAndRelease(t, c2, makeLocalRepo(t, "r3"))
	if got := countBareRepos(t, base); got != 2 {
		t.Errorf("post-restart cached = %d, want 2 (bound must survive restart)", got)
	}
}
