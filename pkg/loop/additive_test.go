package loop

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// alwaysGreen is a test command that passes no matter what — the normal state of
// a healthy repository, and the state additive work starts from.
func alwaysGreen(ctx context.Context) (string, bool, error) {
	return "ok\t\tgithub.com/acme/lib\t0.2s", true, nil
}

// "add one more example in example dir" reported SUCCEEDED, opened no PR, and
// never called the model once. `go test ./...` passed on the unmodified
// repository, so the loop concluded the task was already satisfied and returned
// at step 0.
//
// That is only sound if the test command expresses the request. It does for a
// bug fix — the failing test IS the bug — and not at all for additive work,
// because adding an example does not make the suite start failing. The tests
// are a guard, not the goal.
func TestRun_GreenSuiteStillDoesTheWork(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "example.go")
	if err := os.WriteFile(path, []byte("package example\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	prov := &scriptedProvider{edits: []string{"package example\n\n// NEW EXAMPLE\n"}}
	r := &Runner{Provider: prov, Config: Config{MaxSteps: 3}}

	res, err := r.Run(context.Background(), Task{
		Description: "add one more example",
		FilePath:    path,
	}, alwaysGreen)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.Success {
		t.Fatalf("expected success, got %+v", res)
	}
	if prov.calls == 0 {
		t.Fatal("the Actor was never asked — a passing suite is not a reason to skip the work")
	}
	if res.Steps == 0 {
		t.Errorf("expected at least one Actor step, got %d", res.Steps)
	}

	b, _ := os.ReadFile(path)
	if !strings.Contains(string(b), "NEW EXAMPLE") {
		t.Errorf("the requested change was not written: %q", string(b))
	}
}

// The other direction still works exactly as before: a failing suite is fixed.
func TestRun_RedSuiteIsStillFixed(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "target.txt")
	if err := os.WriteFile(path, []byte("broken"), 0o644); err != nil {
		t.Fatal(err)
	}

	prov := &scriptedProvider{edits: []string{"FIXED"}}
	r := &Runner{Provider: prov, Config: Config{MaxSteps: 3}}

	res, err := r.Run(context.Background(), Task{Description: "fix it", FilePath: path},
		passWhenContains(path, "FIXED"))
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !res.Success || res.Steps != 1 {
		t.Errorf("expected success in one step, got %+v", res)
	}
}

// A change that breaks a previously-green suite must not be reported as
// success — that is the whole reason the tests are still run.
func TestRun_BreakingAGreenSuiteIsNotSuccess(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "f.go")
	if err := os.WriteFile(path, []byte("good"), 0o644); err != nil {
		t.Fatal(err)
	}

	prov := &scriptedProvider{edits: []string{"broken", "broken", "broken"}}
	r := &Runner{Provider: prov, Config: Config{MaxSteps: 2}}

	// Green until the file says "broken".
	gate := func(ctx context.Context) (string, bool, error) {
		b, err := os.ReadFile(path)
		if err != nil {
			return "", false, err
		}
		if strings.Contains(string(b), "broken") {
			return "FAIL: compile error", false, nil
		}
		return "ok", true, nil
	}

	res, err := r.Run(context.Background(), Task{Description: "change it", FilePath: path}, gate)
	if err == nil && res.Success {
		t.Error("a change that breaks the suite must not report success")
	}
}

// Anti-gaming, preserved exactly where it mattered: while the tests are failing,
// a failing test defines the job, and editing it would let the fix be faked.
func TestRun_RefusesToEditAFailingTest(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "thing_test.go")
	if err := os.WriteFile(path, []byte("func TestX(t *testing.T) { t.Fatal() }"), 0o644); err != nil {
		t.Fatal(err)
	}

	prov := &scriptedProvider{edits: []string{"weakened"}}
	r := &Runner{Provider: prov, Config: Config{MaxSteps: 2}}

	_, err := r.Run(context.Background(), Task{
		Description: "make the test pass", FilePath: path, TargetsTest: true,
	}, func(ctx context.Context) (string, bool, error) { return "FAIL", false, nil })

	if err == nil {
		t.Fatal("editing a failing test must be refused")
	}
	if !strings.Contains(err.Error(), "weakening") {
		t.Errorf("the refusal should explain why, got %v", err)
	}
	if prov.calls != 0 {
		t.Errorf("the Actor should not have been called, got %d call(s)", prov.calls)
	}
}

// ...but "add tests for the parser" is an ordinary request. The blanket refusal
// rejected it outright; with a green suite the test file defines nothing, so
// there is nothing to game.
func TestRun_WritingTestsIsAllowedWhenTheSuiteIsGreen(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "parser_test.go")
	if err := os.WriteFile(path, []byte("package p\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	prov := &scriptedProvider{edits: []string{"package p\n\nfunc TestParse(t *testing.T) {}\n"}}
	r := &Runner{Provider: prov, Config: Config{MaxSteps: 2}}

	res, err := r.Run(context.Background(), Task{
		Description: "add tests for the parser", FilePath: path, TargetsTest: true,
	}, alwaysGreen)
	if err != nil {
		t.Fatalf("writing tests against a green suite must be allowed, got %v", err)
	}
	if !res.Success || prov.calls == 0 {
		t.Errorf("expected the work to be done, got %+v after %d call(s)", res, prov.calls)
	}
}

// With a green suite the raw output reads "ok" everywhere, which invites the
// model to conclude there is nothing to do. The Actor has to be told the suite
// is a constraint rather than the objective.
func TestActorContext_GreenSuiteIsFramedAsAConstraint(t *testing.T) {
	got := actorContext("ok\tgithub.com/acme/lib", true)
	if !strings.Contains(got, "PASSES") {
		t.Errorf("the Actor should be told the suite passes, got %q", got)
	}
	if !strings.Contains(strings.ToLower(got), "nothing to do") {
		t.Errorf("the Actor should be warned off the wrong inference, got %q", got)
	}
	if !strings.Contains(got, "ok\tgithub.com/acme/lib") {
		t.Errorf("the real output must still be included, got %q", got)
	}

	// A failing suite needs no framing — the output speaks for itself.
	if got := actorContext("FAIL: TestDivide", false); got != "FAIL: TestDivide" {
		t.Errorf("a red suite should pass through unchanged, got %q", got)
	}
}
