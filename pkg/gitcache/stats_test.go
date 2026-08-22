package gitcache

import "testing"

func TestCacheStatsReflectsRepoCount(t *testing.T) {
	c, cleanup := setupTestCache(t) // existing helper from cache_test.go
	defer cleanup()

	repo := makeLocalRepo(t, "stats-repo") // existing helper from eviction_test.go
	getAndRelease(t, c, repo)              // existing helper from eviction_test.go

	stats := c.Stats()
	if stats.TotalRepos != 1 {
		t.Fatalf("TotalRepos = %d, want 1", stats.TotalRepos)
	}
	if stats.TotalActiveWorktrees != 0 {
		t.Fatalf("TotalActiveWorktrees = %d, want 0 (released)", stats.TotalActiveWorktrees)
	}
}

func TestCacheStatsCountsHitsAndMisses(t *testing.T) {
	c, cleanup := setupTestCache(t)
	defer cleanup()
	repo := makeLocalRepo(t, "hit-repo")

	getAndRelease(t, c, repo) // first fetch: a miss (repo not yet cached)
	getAndRelease(t, c, repo) // second fetch: a hit (bare clone already present)

	stats := c.Stats()
	if stats.MissCount != 1 || stats.HitCount != 1 {
		t.Fatalf("HitCount=%d MissCount=%d, want 1/1", stats.HitCount, stats.MissCount)
	}
}
