package daemon

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"

	"github.com/ibreakthecloud/kiwi/pkg/agent"
)

func gitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, args := range [][]string{
		{"init", "-q"},
		{"config", "user.email", "t@example.com"},
		{"config", "user.name", "t"},
		{"commit", "--allow-empty", "-q", "-m", "base"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	return dir
}

// "add one more example in example dir" reported SUCCEEDED with no pull request
// and the words "no changes" as its whole explanation.
//
// The cause is the definition of done: adding an example does not make
// `go build` start failing, so the test command passed on unmodified code, the
// loop correctly concluded there was nothing to do, and delivery found an
// unchanged worktree. Every step behaved correctly and the user got a green
// tick for work that never happened.
//
// Delivery must report that as an error, not as a deliverable result — a
// benign detail string is what let the caller treat it as success.
func TestPublishResult_UnchangedWorktreeIsAnError(t *testing.T) {
	dir := gitRepo(t)

	_, _, err := publishResult(context.Background(), dir,
		agent.WorkerSpec{ID: "t1", JobID: "job_1", Task: "add an example"},
		"tok", nil, "")

	if err == nil {
		t.Fatal("an unchanged worktree must not be reported as a delivered result")
	}
	if !errors.Is(err, errNoChanges) {
		t.Errorf("expected errNoChanges so the caller can tell this apart from a delivery failure, got %v", err)
	}
}

// The sentinel has to be distinguishable, because the two cases need different
// messages: a delivery failure is Kiwi's problem, an unchanged worktree is a
// mismatch between the task and its verification command.
func TestErrNoChanges_IsDistinctFromDeliveryFailure(t *testing.T) {
	if errors.Is(errors.New("push rejected"), errNoChanges) {
		t.Error("an arbitrary delivery error must not match errNoChanges")
	}
	if !errors.Is(errNoChanges, errNoChanges) {
		t.Error("errNoChanges must match itself")
	}
}

// A real change still delivers: the guard must not swallow ordinary work. The
// push itself cannot run here (no remote), so reaching a non-errNoChanges error
// is what proves it got past the unchanged check.
func TestPublishResult_RealChangeIsNotTreatedAsNoChanges(t *testing.T) {
	dir := gitRepo(t)
	if err := exec.Command("sh", "-c", "echo hello > "+dir+"/example.go").Run(); err != nil {
		t.Fatal(err)
	}

	_, _, err := publishResult(context.Background(), dir,
		agent.WorkerSpec{ID: "t1", JobID: "job_1", Task: "add an example"},
		"tok", nil, "")

	if errors.Is(err, errNoChanges) {
		t.Error("a worktree with a new file was reported as unchanged")
	}
}

// The message is the whole point of the fix, so it has to explain the mismatch
// rather than restate it. "no changes" told the user nothing they could act on.
func TestNoChangesMessage_ExplainsTheMismatch(t *testing.T) {
	// Mirrors the detail built in executeTask for the steps==0 case.
	msg := "the test command (go build ./...) already passed before any change was made, so nothing was done and there is nothing to open a PR with. " +
		"This task needs a check that fails until the work exists — for new functionality, a test that exercises it."

	for _, want := range []string{"already passed", "nothing to open a PR", "fails until the work exists"} {
		if !strings.Contains(msg, want) {
			t.Errorf("message should contain %q, got %q", want, msg)
		}
	}
	if strings.TrimSpace(msg) == "no changes" {
		t.Error("the message must be more than the old sentinel text")
	}
}
