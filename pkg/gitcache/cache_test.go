package gitcache

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// A dummy repo to test with. In a real environment, you'd use a local mock git repo
// to avoid network calls, but for testing the scaffold we'll use a fast, public repo.
const testRepo = "https://github.com/ibreakthecloud/kiwi"

func setupTestCache(t *testing.T) (*Cache, func()) {
	dir, err := os.MkdirTemp("", "kiwi-gitcache-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}

	cache, err := New(dir)
	if err != nil {
		os.RemoveAll(dir)
		t.Fatalf("failed to create cache: %v", err)
	}

	return cache, func() { os.RemoveAll(dir) }
}

func TestCache_GetWorktree_HappyPathAndReuse(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping network test in short mode")
	}
	cache, cleanup := setupTestCache(t)
	defer cleanup()

	ctx := context.Background()
	wt1 := filepath.Join(cache.baseDir, "wt1")

	// 1. First get (clones bare repo)
	err := cache.GetWorktree(ctx, testRepo, "main", wt1)
	if err != nil {
		t.Fatalf("first GetWorktree failed: %v", err)
	}

	// 2. Cache reuse (should be very fast, just worktree add)
	wt2 := filepath.Join(cache.baseDir, "wt2")
	err = cache.GetWorktree(ctx, testRepo, "main", wt2)
	if err != nil {
		t.Fatalf("second GetWorktree failed: %v", err)
	}
}

func TestCache_GetWorktree_InvalidRef(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping network test in short mode")
	}
	cache, cleanup := setupTestCache(t)
	defer cleanup()

	ctx := context.Background()
	wt := filepath.Join(cache.baseDir, "wt_invalid")

	err := cache.GetWorktree(ctx, testRepo, "this-ref-does-not-exist-12345", wt)
	if err == nil {
		t.Fatalf("expected error for invalid ref, got nil")
	}
}

func TestCache_GetWorktree_Concurrency(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping network test in short mode")
	}
	cache, cleanup := setupTestCache(t)
	defer cleanup()

	ctx := context.Background()

	// Spawn multiple concurrent requests for the same repo
	const concurrency = 5
	var wg sync.WaitGroup
	errs := make(chan error, concurrency)

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			wt := filepath.Join(cache.baseDir, fmt.Sprintf("wt_concurrent_%d", i))
			if err := cache.GetWorktree(ctx, testRepo, "main", wt); err != nil {
				errs <- fmt.Errorf("worker %d failed: %v", i, err)
			}
		}(i)
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		if err != nil {
			t.Errorf("concurrent error: %v", err)
		}
	}
}
