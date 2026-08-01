package daemon

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/ibreakthecloud/kiwi/pkg/session"
)

// gitWorkspace is session.Workspace over a real git worktree.
//
// It lives in the daemon rather than in pkg/session for the reason the whole
// trust boundary rests on: git is the one operation that needs the credential
// the sandbox is not allowed to hold, so it runs in the daemon process, outside
// the container, and the Implementer gets no git tool at all.
type gitWorkspace struct {
	root string
	// base is the commit the session started from. Every diff is taken against
	// it, so the reviewer always sees the whole task's change rather than the
	// most recent round's slice.
	base string
}

func (w *gitWorkspace) git(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = w.root
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git %s: %w\n%s", strings.Join(args, " "), err, out)
	}
	return string(out), nil
}

func (w *gitWorkspace) Tree(ctx context.Context) ([]string, error) {
	// repoTree already knows which directories are noise and how large a listing
	// may get; reusing it keeps the Architect's view of the repository identical
	// to the one the single-file path's discovery step uses.
	return repoTree(w.root)
}

// Diff reports the whole task's change, committed rounds and the current
// working tree together.
//
// `git diff <base>` compares the working tree against a commit, so it covers
// both without needing two commands. The intent-to-add pass before it is what
// makes new files appear: an untracked file is invisible to git diff, and a
// session whose entire output is a new file would otherwise show the reviewer
// an empty diff and be refused for producing nothing.
func (w *gitWorkspace) Diff(ctx context.Context) (string, error) {
	if _, err := w.git(ctx, "add", "-A", "-N"); err != nil {
		return "", err
	}
	return w.git(ctx, "diff", w.base)
}

func (w *gitWorkspace) FilesChanged(ctx context.Context) ([]string, error) {
	if _, err := w.git(ctx, "add", "-A", "-N"); err != nil {
		return nil, err
	}
	out, err := w.git(ctx, "diff", "--name-only", w.base)
	if err != nil {
		return nil, err
	}
	var files []string
	for _, line := range strings.Split(out, "\n") {
		if s := strings.TrimSpace(line); s != "" {
			files = append(files, s)
		}
	}
	return files, nil
}

func (w *gitWorkspace) HeadSHA(ctx context.Context) (string, error) {
	out, err := w.git(ctx, "rev-parse", "HEAD")
	return strings.TrimSpace(out), err
}

// Commit records the round's work. It returns session.ErrNoChanges when there
// is nothing to record, which the session treats as a round that produced
// nothing rather than as a failure.
func (w *gitWorkspace) Commit(ctx context.Context, message string) (string, error) {
	if _, err := w.git(ctx, "add", "-A"); err != nil {
		return "", err
	}
	status, err := w.git(ctx, "status", "--porcelain")
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(status) == "" {
		return "", session.ErrNoChanges
	}
	if _, err := w.git(ctx, "-c", "user.email=bot@runkiwi.dev", "-c", "user.name=Kiwi", "commit", "-m", message); err != nil {
		return "", err
	}
	return w.HeadSHA(ctx)
}

// Reset discards everything back to sha. It is how a resumed session throws
// away a partially-completed round rather than trying to continue one.
func (w *gitWorkspace) Reset(ctx context.Context, sha string) error {
	_, err := w.git(ctx, "reset", "--hard", sha)
	return err
}
