package gitcache

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Cache manages local bare repositories and isolated git worktrees.
//
// Bare clones are expensive to create (a full network fetch) and cheap to keep,
// so the cache favors reuse. But an always-on daemon that clones a new repo per
// distinct task would grow its disk without bound. The cache therefore enforces
// a bound on the number of cached bare repositories, evicting the
// least-frequently-used repo when a new clone would exceed it (LFU, with
// least-recently-used as the tiebreak).
//
// A repo with live worktrees is never evicted — doing so would pull the ground
// out from under a running task. Liveness is tracked by an in-process counter;
// it resets to zero on restart, which is safe because a fresh process has no
// running tasks and any worktrees left on disk from a previous run are stale.
//
// The bound is enforced eventually, not instantaneously. Eviction never removes
// a repo that another goroutine is actively cloning (that repo's lock is held),
// so a burst of concurrent clones of distinct new repos can transiently exceed
// the bound; the cache converges back once the churn settles and the next
// access runs eviction. The daemon clones sequentially per task, so in practice
// the bound holds tightly — the eventual guarantee only matters under an
// artificial concurrent cold-start burst.
type Cache struct {
	baseDir string
	// maxRepos bounds the number of cached bare repos. 0 means unbounded.
	maxRepos int

	locks sync.Map // barePath -> *sync.Mutex, serializes git ops on one repo

	mu   sync.Mutex           // guards meta
	meta map[string]*repoMeta // barePath -> access metadata
}

// repoMeta is the LFU bookkeeping for one cached bare repo.
type repoMeta struct {
	freq       uint64    // access count; the LFU eviction key
	lastAccess time.Time // tiebreak when frequencies are equal (LRU)
	active     int       // live worktrees; a repo with active > 0 is not evictable
}

// New creates an unbounded Cache (no eviction). Prefer NewWithLimit for any
// long-lived process.
func New(baseDir string) (*Cache, error) {
	return NewWithLimit(baseDir, 0)
}

// NewWithLimit creates a Cache that keeps at most maxRepos bare repositories
// (0 = unbounded). Existing bare repos already on disk are picked up so the
// bound is enforced across restarts.
func NewWithLimit(baseDir string, maxRepos int) (*Cache, error) {
	if err := os.MkdirAll(baseDir, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create cache dir: %w", err)
	}
	c := &Cache{
		baseDir:  baseDir,
		maxRepos: maxRepos,
		meta:     make(map[string]*repoMeta),
	}
	c.loadExisting()
	return c, nil
}

// loadExisting seeds meta from bare repos already on disk so a restarted daemon
// still honors the bound. Their repo URLs are not recoverable from the hashed
// directory name, and their true access frequency is lost, so each is seeded
// with freq 0 and its directory mtime — i.e. treated as a cold, old entry and
// thus a preferred eviction candidate until it is used again.
func (c *Cache) loadExisting() {
	entries, err := os.ReadDir(c.baseDir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		barePath := filepath.Join(c.baseDir, e.Name())
		// A bare clone has a HEAD file at its root; skip anything else (e.g. a
		// worktrees/ scratch dir the daemon may keep alongside).
		if _, err := os.Stat(filepath.Join(barePath, "HEAD")); err != nil {
			continue
		}
		info, err := e.Info()
		last := time.Now()
		if err == nil {
			last = info.ModTime()
		}
		c.meta[barePath] = &repoMeta{freq: 0, lastAccess: last}
	}
}

// repoPath generates a unique, filesystem-safe path for the bare clone.
func (c *Cache) repoPath(repoURL string) string {
	hash := sha256.Sum256([]byte(repoURL))
	return filepath.Join(c.baseDir, fmt.Sprintf("%x", hash[:16]))
}

// getRepoLock returns the mutex serializing git operations on a bare repo,
// keyed by its on-disk path so eviction (which knows only the path) and
// GetWorktree (which knows the URL) contend on the same lock.
func (c *Cache) getRepoLock(barePath string) *sync.Mutex {
	v, _ := c.locks.LoadOrStore(barePath, &sync.Mutex{})
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
	barePath := c.repoPath(repoURL)
	mu := c.getRepoLock(barePath)
	mu.Lock()
	defer mu.Unlock()

	// 1. Ensure bare clone exists
	if _, err := os.Stat(barePath); os.IsNotExist(err) {
		// A new repo is about to be cloned. Make room first so the bound holds.
		c.evictToFit(barePath)
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
		c.recordAccess(barePath, +1)
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

	c.recordAccess(barePath, +1)
	return nil
}

// GetJobWorktree provisions a worktree for a worker of a job, basing it on the
// job's shared branch (jobBranch) when that branch already exists on the remote,
// and otherwise on baseRef. This is what makes workers compose (issue #126): the
// scheduler runs a job's workers in dependency order, so by the time a later
// worker leases, earlier workers have already committed to jobBranch — basing on
// it means the later worker sees their edits, and its commit fast-forwards the
// branch rather than forking a disconnected diff. The first worker (no branch
// yet) falls back to baseRef. jobBranch == "" behaves exactly like GetWorktree.
func (c *Cache) GetJobWorktree(ctx context.Context, repoURL, baseRef, jobBranch, worktreePath string) error {
	if jobBranch == "" {
		return c.GetWorktree(ctx, repoURL, baseRef, worktreePath)
	}

	barePath := c.repoPath(repoURL)
	mu := c.getRepoLock(barePath)
	mu.Lock()
	defer mu.Unlock()

	if _, err := os.Stat(barePath); os.IsNotExist(err) {
		c.evictToFit(barePath)
		if err := runGit(ctx, "", "clone", "--bare", repoURL, barePath); err != nil {
			return fmt.Errorf("failed to clone bare repo: %w", err)
		}
	} else if err != nil {
		return fmt.Errorf("failed to check cache repo stat: %w", err)
	}

	os.RemoveAll(worktreePath)
	runGit(ctx, barePath, "worktree", "prune")

	// Prefer the shared job branch: if it exists on the remote, base the worktree
	// on its tip (which carries earlier workers' commits).
	if err := runGit(ctx, barePath, "fetch", "origin", jobBranch); err == nil {
		if err := runGit(ctx, barePath, "worktree", "add", "--detach", worktreePath, "FETCH_HEAD"); err == nil {
			c.recordAccess(barePath, +1)
			return nil
		}
		os.RemoveAll(worktreePath)
	}

	// First worker (or no job branch yet): base on the requested ref, mirroring
	// GetWorktree's add-then-fetch fallback.
	if err := runGit(ctx, barePath, "worktree", "add", "--detach", worktreePath, baseRef); err == nil {
		c.recordAccess(barePath, +1)
		return nil
	}
	if err := runGit(ctx, barePath, "fetch", "origin", baseRef); err != nil {
		return fmt.Errorf("failed to fetch base ref %s: %w", baseRef, err)
	}
	if err := runGit(ctx, barePath, "worktree", "add", "--detach", worktreePath, "FETCH_HEAD"); err != nil {
		os.RemoveAll(worktreePath)
		return fmt.Errorf("failed to add worktree after fetch: %w", err)
	}
	c.recordAccess(barePath, +1)
	return nil
}

// RemoveWorktree safely removes a worktree and cleans up the bare clone's worktree list.
func (c *Cache) RemoveWorktree(ctx context.Context, repoURL, worktreePath string) error {
	barePath := c.repoPath(repoURL)
	mu := c.getRepoLock(barePath)
	mu.Lock()
	defer mu.Unlock()

	// Remove the worktree explicitly.
	// Since we are not in the worktree itself, we pass the path to `git worktree remove`
	if err := runGit(ctx, barePath, "worktree", "remove", "--force", worktreePath); err != nil {
		return fmt.Errorf("failed to remove worktree: %w", err)
	}

	// The repo now has one fewer live worktree, making it eligible for eviction
	// once its active count reaches zero.
	c.recordActive(barePath, -1)
	return nil
}

// recordAccess bumps a repo's LFU frequency and last-access time, and adjusts
// its live-worktree count by activeDelta. It creates the meta entry on first use.
func (c *Cache) recordAccess(barePath string, activeDelta int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	m := c.meta[barePath]
	if m == nil {
		m = &repoMeta{}
		c.meta[barePath] = m
	}
	m.freq++
	m.lastAccess = time.Now()
	m.active += activeDelta
	if m.active < 0 {
		m.active = 0
	}
}

// recordActive adjusts only the live-worktree count (no frequency bump).
func (c *Cache) recordActive(barePath string, delta int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if m := c.meta[barePath]; m != nil {
		m.active += delta
		if m.active < 0 {
			m.active = 0
		}
	}
}

// evictToFit removes least-frequently-used bare repos until adding one more
// (keepPath, the repo about to be cloned) would stay within maxRepos. It never
// evicts a repo with live worktrees, and never evicts keepPath itself.
//
// Called while holding keepPath's repo lock. To touch a victim's files safely
// it takes that victim's lock with TryLock and skips any it cannot acquire —
// so it can never block on, or deadlock against, a concurrent GetWorktree.
func (c *Cache) evictToFit(keepPath string) {
	if c.maxRepos <= 0 {
		return
	}
	// A candidate this pass could not evict (its file lock was held) is recorded
	// here so we stop instead of spinning on it.
	skipped := make(map[string]bool)
	for {
		victim := c.pickVictim(keepPath, skipped)
		if victim == "" {
			return // nothing evictable (all busy, or already within bound)
		}
		vlock := c.getRepoLock(victim)
		if !vlock.TryLock() {
			// Busy right now: another goroutine holds it. Skip it and move on so
			// we can never block on, or deadlock against, a concurrent caller.
			skipped[victim] = true
			continue
		}
		// Re-check under the meta lock that it is still evictable, now that we
		// hold its file lock (its active count may have changed).
		if c.stillEvictable(victim) {
			if err := os.RemoveAll(victim); err != nil {
				log.Printf("gitcache: failed to evict %s: %v", victim, err)
			}
			c.dropMeta(victim)
		} else {
			skipped[victim] = true
		}
		vlock.Unlock()
	}
}

// pickVictim returns the barePath of the least-frequently-used evictable repo,
// or "" if the cache is within bound or nothing is evictable. Eviction is
// needed when the current count would exceed the bound once keepPath is added;
// keepPath (the incoming repo) and anything in skipped are excluded.
func (c *Cache) pickVictim(keepPath string, skipped map[string]bool) string {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Count entries other than keepPath; the incoming clone will occupy one slot.
	others := 0
	for path := range c.meta {
		if path != keepPath {
			others++
		}
	}
	if others < c.maxRepos {
		return ""
	}

	var victim string
	var best *repoMeta
	for path, m := range c.meta {
		if path == keepPath || m.active > 0 || skipped[path] {
			continue
		}
		if best == nil || m.freq < best.freq ||
			(m.freq == best.freq && m.lastAccess.Before(best.lastAccess)) {
			victim, best = path, m
		}
	}
	return victim
}

// stillEvictable re-checks under the meta lock that a repo has no live worktrees.
func (c *Cache) stillEvictable(barePath string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	m := c.meta[barePath]
	return m != nil && m.active == 0
}

// dropMeta removes a repo's bookkeeping after its files are gone.
func (c *Cache) dropMeta(barePath string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.meta, barePath)
	c.locks.Delete(barePath)
}
