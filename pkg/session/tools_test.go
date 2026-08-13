package session

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ibreakthecloud/kiwi/pkg/provider"
)

func newTools(t *testing.T) (*FileTools, string) {
	t.Helper()
	root := t.TempDir()
	mustWrite(t, root, "main.go", "package main\n\nfunc main() {}\n")
	mustWrite(t, root, "internal/fetch/client.go", "package fetch\n\n// TODO: retry\n")
	mustWrite(t, root, ".git/config", "[core]\n")
	return &FileTools{Root: root}, root
}

func mustWrite(t *testing.T, root, rel, content string) {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func call(t *testing.T, ft *FileTools, name string, args any) provider.ToolResult {
	t.Helper()
	res, err := ft.Call(context.Background(), provider.MockCall("c", name, args))
	if err != nil {
		t.Fatalf("%s returned an infrastructure error: %v", name, err)
	}
	return res
}

func TestReadAndWriteRoundTrip(t *testing.T) {
	ft, root := newTools(t)

	got := call(t, ft, ToolReadFile, map[string]string{"path": "main.go"})
	if got.IsError || !strings.Contains(got.Content, "func main") {
		t.Fatalf("read failed: %+v", got)
	}

	res := call(t, ft, ToolWriteFile, map[string]string{"path": "pkg/new/thing.go", "content": "package thing\n"})
	if res.IsError {
		t.Fatalf("write failed: %s", res.Content)
	}
	b, err := os.ReadFile(filepath.Join(root, "pkg/new/thing.go"))
	if err != nil {
		t.Fatalf("parent directories should have been created: %v", err)
	}
	if string(b) != "package thing\n" {
		t.Errorf("content = %q", b)
	}
}

// The daemon owns git because git is the one operation needing the credential
// the sandbox may not hold. An agent that can rewrite .git can rewrite what is
// about to be pushed.
func TestGitDirectoryIsRefused(t *testing.T) {
	ft, _ := newTools(t)

	for _, path := range []string{".git/config", ".git", ".git/hooks/pre-commit"} {
		read := call(t, ft, ToolReadFile, map[string]string{"path": path})
		if !read.IsError {
			t.Errorf("reading %q should be refused, got %q", path, read.Content)
		}
		write := call(t, ft, ToolWriteFile, map[string]string{"path": path, "content": "x"})
		if !write.IsError {
			t.Errorf("writing %q should be refused", path)
		}
	}
}

func TestPathsCannotEscapeTheWorktree(t *testing.T) {
	ft, _ := newTools(t)

	for _, path := range []string{"../outside.txt", "../../etc/passwd", "/etc/passwd"} {
		read := call(t, ft, ToolReadFile, map[string]string{"path": path})
		if !read.IsError {
			t.Errorf("reading %q should be refused, got %q", path, read.Content)
		}
		write := call(t, ft, ToolWriteFile, map[string]string{"path": path, "content": "x"})
		if !write.IsError {
			t.Errorf("writing %q should be refused", path)
		}
	}
}

// A model that echoes back an absolute path inside the worktree is being
// helpful, not hostile; refusing it would cost a round to protocol.
func TestAbsolutePathInsideTheWorktreeIsAccepted(t *testing.T) {
	ft, root := newTools(t)
	got := call(t, ft, ToolReadFile, map[string]string{"path": filepath.Join(root, "main.go")})
	if got.IsError {
		t.Fatalf("an absolute path inside the worktree should resolve: %s", got.Content)
	}
}

func TestGrepReportsFileAndLine(t *testing.T) {
	ft, _ := newTools(t)
	got := call(t, ft, ToolGrep, map[string]string{"pattern": "TODO"})
	if got.IsError {
		t.Fatalf("grep failed: %s", got.Content)
	}
	if !strings.Contains(got.Content, "internal/fetch/client.go:3:") {
		t.Errorf("expected a file:line match, got %q", got.Content)
	}
}

func TestGrepRejectsABadPattern(t *testing.T) {
	ft, _ := newTools(t)
	got := call(t, ft, ToolGrep, map[string]string{"pattern": "("})
	if !got.IsError {
		t.Fatal("an invalid regular expression should be reported as a tool error")
	}
}

func TestListSkipsNoiseDirectories(t *testing.T) {
	ft, root := newTools(t)
	mustWrite(t, root, "node_modules/left-pad/index.js", "module.exports = 1\n")

	got := call(t, ft, ToolListFiles, map[string]string{})
	if strings.Contains(got.Content, "node_modules") {
		t.Errorf("listing should skip node_modules, got %q", got.Content)
	}
	if strings.Contains(got.Content, ".git/") {
		t.Errorf("listing should skip .git, got %q", got.Content)
	}
	if !strings.Contains(got.Content, "main.go") {
		t.Errorf("listing should include real files, got %q", got.Content)
	}
}

// A failing command is feedback, not an infrastructure failure — the same
// distinction loop.TestFunc draws, and the round must continue.
func TestRunReportsCommandFailureAsToolError(t *testing.T) {
	ft, _ := newTools(t)
	ft.Exec = func(ctx context.Context, cmd string) (string, bool, error) {
		return "exit status 1\nundefined: foo", false, nil
	}

	got := call(t, ft, ToolRun, map[string]string{"command": "go build ./..."})
	if !got.IsError {
		t.Fatal("a non-zero exit should be IsError so the model reacts to it")
	}
	if !strings.Contains(got.Content, "undefined: foo") {
		t.Errorf("output not passed through: %q", got.Content)
	}
}

// A broken sandbox is not something the model can act on, so it aborts the
// round rather than becoming another tool result.
func TestRunPropagatesSandboxFailure(t *testing.T) {
	ft, _ := newTools(t)
	ft.Exec = func(ctx context.Context, cmd string) (string, bool, error) {
		return "", false, context.DeadlineExceeded
	}
	if _, err := ft.Call(context.Background(), provider.MockCall("c", ToolRun, map[string]string{"command": "x"})); err == nil {
		t.Fatal("a sandbox failure must be returned as an error")
	}
}

func TestToolsAreOnlyOfferedWhenTheyCanWork(t *testing.T) {
	ft, _ := newTools(t)
	if names := defNames(ft.Defs()); contains(names, ToolRun) || contains(names, ToolInstall) {
		t.Fatalf("run/install must not be offered without an executor: %v", names)
	}

	ft.Exec = func(ctx context.Context, cmd string) (string, bool, error) { return "", true, nil }
	ft.Install = func(ctx context.Context) (string, bool, error) { return "", true, nil }
	names := defNames(ft.Defs())
	if !contains(names, ToolRun) || !contains(names, ToolInstall) {
		t.Fatalf("run/install should be offered once wired: %v", names)
	}
}

func TestFinishCapturesTheHandoffNote(t *testing.T) {
	ft, _ := newTools(t)
	if done, _ := ft.Finished(); done {
		t.Fatal("a fresh round must not start finished")
	}

	call(t, ft, ToolFinish, map[string]string{"note": "added the retry wrapper"})
	done, report := ft.Finished()
	if !done || report.Note != "added the retry wrapper" {
		t.Fatalf("finish not recorded: done=%v note=%q", done, report.Note)
	}

	ft.Reset()
	if done, _ := ft.Finished(); done {
		t.Fatal("Reset must clear the finish state so the next round starts clean")
	}
}

func TestFinishCapturesAnswersNewQuestionsAndDecisions(t *testing.T) {
	ft, _ := newTools(t)

	call(t, ft, ToolFinish, map[string]any{
		"note":          "switched the store to Postgres",
		"answers":       []string{"the fs backend is unused elsewhere, safe to drop"},
		"new_questions": []string{"should the migration run for existing rows too?"},
		"decisions":     []string{"kept the interface pkg/store already expects"},
	})

	done, report := ft.Finished()
	if !done {
		t.Fatal("finish should have been recorded")
	}
	if len(report.Answers) != 1 || report.Answers[0] != "the fs backend is unused elsewhere, safe to drop" {
		t.Errorf("answers not captured: %#v", report.Answers)
	}
	if len(report.NewQuestions) != 1 || report.NewQuestions[0] != "should the migration run for existing rows too?" {
		t.Errorf("new_questions not captured: %#v", report.NewQuestions)
	}
	if len(report.Decisions) != 1 || report.Decisions[0] != "kept the interface pkg/store already expects" {
		t.Errorf("decisions not captured: %#v", report.Decisions)
	}

	ft.Reset()
	if _, report := ft.Finished(); len(report.Answers) != 0 {
		t.Fatal("Reset must clear the report, not just the finished flag")
	}
}

func TestOversizedWriteIsRefused(t *testing.T) {
	ft, _ := newTools(t)
	ft.MaxWriteBytes = 16
	got := call(t, ft, ToolWriteFile, map[string]string{"path": "big.txt", "content": strings.Repeat("x", 100)})
	if !got.IsError {
		t.Fatal("an oversized write should be refused")
	}
}

// A model that cats a huge file must get a truncated answer rather than a round
// that dies on the provider's input limit — and the tail is the useful end.
func TestToolOutputIsTruncatedKeepingTheTail(t *testing.T) {
	ft, root := newTools(t)
	ft.MaxOutputBytes = 100
	mustWrite(t, root, "big.txt", strings.Repeat("a", 500)+"THE-END")

	got := call(t, ft, ToolReadFile, map[string]string{"path": "big.txt"})
	if len(got.Content) > 300 {
		t.Errorf("output not truncated: %d bytes", len(got.Content))
	}
	if !strings.Contains(got.Content, "THE-END") {
		t.Error("truncation should keep the tail")
	}
}

func TestUnknownToolIsReportedNotFatal(t *testing.T) {
	ft, _ := newTools(t)
	got := call(t, ft, "teleport", map[string]string{})
	if !got.IsError || !strings.Contains(got.Content, "teleport") {
		t.Fatalf("unknown tool should be a tool error naming it, got %+v", got)
	}
}

func defNames(defs []provider.ToolDef) []string {
	out := make([]string, len(defs))
	for i, d := range defs {
		out[i] = d.Name
	}
	return out
}

func contains(hay []string, needle string) bool {
	for _, h := range hay {
		if h == needle {
			return true
		}
	}
	return false
}
