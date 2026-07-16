package gitcache

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

// Cache manages local bare repositories and isolated git worktrees.
//
// NOTE: this cache is currently unbounded — bare repositories are cloned on
// demand and never evicted. A bounded, least-frequently-used (LFU) eviction
// policy (frequency counter + evict via worktree prune + remove bare dir under
// a size/count bound) is tracked as a follow-up issue.
type Cache struct {
	baseDir string
	locks   sync.Map
}

// New creates a new Cache instance.
func New(baseDir string) (*Cache, error) {
	if err := os.MkdirAll(baseDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create cache dir: %w", err)
	}
	return &Cache{
		baseDir: baseDir,
	}, nil
}

// repoPath generates a unique, filesystem-safe path for the bare clone.
func (c *Cache) repoPath(repoURL string) string {
	hash := sha256.Sum256([]byte(repoURL))
	return filepath.Join(c.baseDir, fmt.Sprintf("%x", hash[:16]))
}

func (c *Cache) getRepoLock(repoURL string) *sync.Mutex {
	v, _ := c.locks.LoadOrStore(repoURL, &sync.Mutex{})
	return v.(*sync.Mutex)
}

// runGit executes a git command and returns its output.
func runGit(ctx context.Context, dir string, args ...string) error {
	cmd := exec.CommandContext(ctx, "git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git %s failed: %w (stderr: %s)", strings.Join(args, " "), err, stderr.String())
	}
	return nil
}

// GetWorktree ensures the repo is cached and creates a new worktree at the target path.
// `ref` can be a branch, tag, or commit hash.
func (c *Cache) GetWorktree(ctx context.Context, repoURL, ref, worktreePath string) error {
	mu := c.getRepoLock(repoURL)
	mu.Lock()
	defer mu.Unlock()

	barePath := c.repoPath(repoURL)

	// 1. Ensure bare clone exists
	if _, err := os.Stat(barePath); os.IsNotExist(err) {
		if err := runGit(ctx, "", "clone", "--bare", repoURL, barePath); err != nil {
			return fmt.Errorf("failed to clone bare repo: %w", err)
		}
	} else if err != nil {
		return fmt.Errorf("failed to check cache repo stat: %w", err)
	}

	// Remove any stale target directory (from a previous failure)
	os.RemoveAll(worktreePath)

	// Prune stale worktrees
	runGit(ctx, barePath, "worktree", "prune")

	// Try adding the worktree with the requested ref immediately (without fetch)
	if err := runGit(ctx, barePath, "worktree", "add", "--detach", worktreePath, ref); err == nil {
		return nil // Success!
	}

	// If it fails, fetch the specific ref and try again
	if err := runGit(ctx, barePath, "fetch", "origin", ref); err != nil {
		return fmt.Errorf("failed to fetch ref %s: %w", ref, err)
	}

	if err := runGit(ctx, barePath, "worktree", "add", "--detach", worktreePath, "FETCH_HEAD"); err != nil {
		// Clean up on error
		os.RemoveAll(worktreePath)
		return fmt.Errorf("failed to add worktree after fetch: %w", err)
	}

	return nil
}

// RemoveWorktree safely removes a worktree and cleans up the bare clone's worktree list.
func (c *Cache) RemoveWorktree(ctx context.Context, repoURL, worktreePath string) error {
	mu := c.getRepoLock(repoURL)
	mu.Lock()
	defer mu.Unlock()

	barePath := c.repoPath(repoURL)

	// Remove the worktree explicitly.
	// Since we are not in the worktree itself, we pass the path to `git worktree remove`
	if err := runGit(ctx, barePath, "worktree", "remove", "--force", worktreePath); err != nil {
		return fmt.Errorf("failed to remove worktree: %w", err)
	}

	return nil
}
