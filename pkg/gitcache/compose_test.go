package gitcache

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestGetJobWorktree_Composition proves the #126 core: a second worker basing on
// the shared job branch sees the first worker's committed edits. It runs fully
// locally against a bare "remote" repo — no network.
func TestGetJobWorktree_Composition(t *testing.T) {
	ctx := context.Background()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.CommandContext(ctx, "git", args...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	// A bare "remote" seeded with one commit on main.
	remote := filepath.Join(t.TempDir(), "remote.git")
	run("init", "-q", "--bare", "-b", "main", remote)
	seed := t.TempDir()
	run("-C", seed, "init", "-q", "-b", "main")
	if err := os.WriteFile(filepath.Join(seed, "base.txt"), []byte("base"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("-C", seed, "add", "-A")
	run("-C", seed, "commit", "-q", "-m", "init")
	run("-C", seed, "push", "-q", remote, "HEAD:refs/heads/main")

	cache, err := New(t.TempDir())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	const jobBranch = "kiwi/job-1"

	// Worker A: no job branch yet → based on main. It commits and pushes the
	// job branch.
	wtA := filepath.Join(t.TempDir(), "wtA")
	if err := cache.GetJobWorktree(ctx, remote, "main", jobBranch, wtA); err != nil {
		t.Fatalf("worker A GetJobWorktree: %v", err)
	}
	if err := os.WriteFile(filepath.Join(wtA, "a.txt"), []byte("A"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("-C", wtA, "add", "-A")
	run("-C", wtA, "commit", "-q", "-m", "worker A")
	run("-C", wtA, "push", "-q", remote, "HEAD:refs/heads/"+jobBranch)

	// Worker B: job branch now exists → based on it → must see A's file.
	wtB := filepath.Join(t.TempDir(), "wtB")
	if err := cache.GetJobWorktree(ctx, remote, "main", jobBranch, wtB); err != nil {
		t.Fatalf("worker B GetJobWorktree: %v", err)
	}
	if _, err := os.Stat(filepath.Join(wtB, "a.txt")); err != nil {
		t.Error("worker B's worktree must contain worker A's committed file (composition)")
	}

	// And B's commit fast-forwards the shared branch (no fork).
	if err := os.WriteFile(filepath.Join(wtB, "b.txt"), []byte("B"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("-C", wtB, "add", "-A")
	run("-C", wtB, "commit", "-q", "-m", "worker B")
	run("-C", wtB, "push", "-q", remote, "HEAD:refs/heads/"+jobBranch) // fast-forward; fails if B had forked
}
