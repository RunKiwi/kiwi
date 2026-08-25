package daemon

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ibreakthecloud/kiwi/pkg/agent"
	"github.com/ibreakthecloud/kiwi/pkg/session"
)

func newWorkspace(t *testing.T) (*gitWorkspace, string) {
	t.Helper()
	dir := gitRepo(t)
	w := &gitWorkspace{root: dir}
	head, err := w.HeadSHA(context.Background())
	if err != nil {
		t.Fatalf("HeadSHA: %v", err)
	}
	w.base = head
	return w, dir
}

func TestWorkspaceCommitAdvancesHeadAndReportsNoChanges(t *testing.T) {
	w, dir := newWorkspace(t)
	ctx := context.Background()

	if _, err := w.Commit(ctx, "nothing"); !errors.Is(err, session.ErrNoChanges) {
		t.Fatalf("an empty commit must report ErrNoChanges, got %v", err)
	}

	if err := os.WriteFile(filepath.Join(dir, "a.go"), []byte("package a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sha, err := w.Commit(ctx, "round 1")
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if sha == w.base {
		t.Fatal("committing must advance the head")
	}
}

// The reviewer must see the accumulated change across rounds, not the last
// round's slice — that isolation is the single-file Critic's central weakness.
func TestWorkspaceDiffSpansEveryRound(t *testing.T) {
	w, dir := newWorkspace(t)
	ctx := context.Background()

	mustFile(t, dir, "one.go", "package one\n")
	if _, err := w.Commit(ctx, "round 1"); err != nil {
		t.Fatal(err)
	}
	mustFile(t, dir, "two.go", "package two\n")
	if _, err := w.Commit(ctx, "round 2"); err != nil {
		t.Fatal(err)
	}

	diff, err := w.Diff(ctx)
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	if !strings.Contains(diff, "one.go") || !strings.Contains(diff, "two.go") {
		t.Errorf("diff should span both rounds, got:\n%s", diff)
	}

	files, err := w.FilesChanged(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 {
		t.Errorf("files changed = %v, want both", files)
	}
}

// A session whose entire output is a new file is the common case for additive
// work. An untracked file is invisible to `git diff`, so without the
// intent-to-add pass the reviewer would see an empty diff and refuse work that
// was actually done.
func TestWorkspaceDiffSeesUncommittedNewFiles(t *testing.T) {
	w, dir := newWorkspace(t)
	mustFile(t, dir, "examples/new.go", "package examples\n")

	diff, err := w.Diff(context.Background())
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	if !strings.Contains(diff, "examples/new.go") {
		t.Errorf("an untracked new file must appear in the diff, got:\n%s", diff)
	}
}

func TestWorkspaceResetDiscardsARound(t *testing.T) {
	w, dir := newWorkspace(t)
	ctx := context.Background()

	mustFile(t, dir, "keep.go", "package keep\n")
	good, err := w.Commit(ctx, "round 1")
	if err != nil {
		t.Fatal(err)
	}

	mustFile(t, dir, "discard.go", "package discard\n")
	if _, err := w.Commit(ctx, "round 2"); err != nil {
		t.Fatal(err)
	}

	if err := w.Reset(ctx, good); err != nil {
		t.Fatalf("Reset: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "discard.go")); !os.IsNotExist(err) {
		t.Error("reset should have discarded the second round's file")
	}
	if _, err := os.Stat(filepath.Join(dir, "keep.go")); err != nil {
		t.Error("reset must not discard earlier rounds")
	}
}

// A session commits as it goes, so by delivery time the worktree is clean and
// the work is already in history. That is indistinguishable to `git status`
// from a run that did nothing, and reporting errNoChanges there would throw
// away a finished task.
func TestPublishFromBaseDeliversAlreadyCommittedWork(t *testing.T) {
	dir := gitRepo(t)
	base := headOf(t, dir)

	mustFile(t, dir, "done.go", "package done\n")
	runGitIn(t, dir, "add", "-A")
	runGitIn(t, dir, "commit", "-q", "-m", "kiwi: round 1")

	bare := t.TempDir()
	runGitIn(t, bare, "init", "--bare", "-q")

	_, _, err := publishResultFrom(context.Background(), dir,
		agent.WorkerSpec{ID: "t1", JobID: "job_1", Task: "add a thing"},
		"", "tok", &fakeGH{}, bare, base)
	if err != nil {
		t.Fatalf("already-committed work must be deliverable: %v", err)
	}
}

// The distinction only holds because of baseSHA. Without one — the single-file
// path — a clean worktree still means nothing was produced.
func TestPublishWithoutBaseKeepsTheNoChangesGuarantee(t *testing.T) {
	dir := gitRepo(t)
	_, _, err := publishResultFrom(context.Background(), dir,
		agent.WorkerSpec{ID: "t1", JobID: "job_1", Task: "add a thing"},
		"", "tok", &fakeGH{}, "", "")
	if !errors.Is(err, errNoChanges) {
		t.Fatalf("expected errNoChanges, got %v", err)
	}
}

// A session that committed nothing must still be refused, even with a base to
// compare against: head has not moved, so there is nothing to open a PR with.
func TestPublishFromBaseStillRefusesAnUntouchedRepo(t *testing.T) {
	dir := gitRepo(t)
	base := headOf(t, dir)

	_, _, err := publishResultFrom(context.Background(), dir,
		agent.WorkerSpec{ID: "t1", JobID: "job_1", Task: "add a thing"},
		"", "tok", &fakeGH{}, "", base)
	if !errors.Is(err, errNoChanges) {
		t.Fatalf("expected errNoChanges, got %v", err)
	}
}

func mustFile(t *testing.T, dir, rel, content string) {
	t.Helper()
	p := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func runGitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return string(out)
}

func headOf(t *testing.T, dir string) string {
	t.Helper()
	return strings.TrimSpace(runGitIn(t, dir, "rev-parse", "HEAD"))
}
