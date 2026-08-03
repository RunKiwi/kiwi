package session

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The tool surface used to offer exactly one way to change a file: write it
// whole. These tests cover its replacement and, as importantly, the cases where
// edit_file must REFUSE — each of which has a quiet, plausible-looking wrong
// answer that the test command would not necessarily catch.

func readFileFirst(t *testing.T, ft *FileTools, path string) {
	t.Helper()
	if res := call(t, ft, ToolReadFile, map[string]string{"path": path}); res.IsError {
		t.Fatalf("precondition read of %s failed: %s", path, res.Content)
	}
}

func TestEditReplacesExactlyOneOccurrence(t *testing.T) {
	ft, root := newTools(t)
	readFileFirst(t, ft, "main.go")

	res := call(t, ft, ToolEditFile, map[string]any{
		"path":       "main.go",
		"old_string": "func main() {}",
		"new_string": "func main() {\n\tprintln(\"hi\")\n}",
	})
	if res.IsError {
		t.Fatalf("edit failed: %s", res.Content)
	}

	b, err := os.ReadFile(filepath.Join(root, "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `println("hi")`) {
		t.Errorf("edit did not land: %q", b)
	}
	// The rest of the file must be untouched — the whole point of editing over
	// rewriting is that nothing outside old_string moves.
	if !strings.HasPrefix(string(b), "package main\n") {
		t.Errorf("edit disturbed the rest of the file: %q", b)
	}
}

func TestEditRefusesAmbiguousMatch(t *testing.T) {
	ft, _ := newTools(t)
	mustWrite(t, ft.Root, "dup.go", "package dup\n\nvar a = 1\nvar b = 1\n")
	readFileFirst(t, ft, "dup.go")

	res := call(t, ft, ToolEditFile, map[string]any{
		"path":       "dup.go",
		"old_string": "= 1",
		"new_string": "= 2",
	})
	if !res.IsError {
		t.Fatal("an ambiguous old_string must be refused, not applied to the first match")
	}
	// The message has to tell the model how to proceed; "ambiguous" alone costs
	// it a turn to guess what to do.
	if !strings.Contains(res.Content, "2 times") || !strings.Contains(res.Content, "unique") {
		t.Errorf("error should say how many matches and what to do, got %q", res.Content)
	}

	b, _ := os.ReadFile(filepath.Join(ft.Root, "dup.go"))
	if strings.Contains(string(b), "= 2") {
		t.Error("a refused edit must not have modified the file")
	}
}

func TestEditReplaceAllAcceptsAmbiguity(t *testing.T) {
	ft, _ := newTools(t)
	mustWrite(t, ft.Root, "dup.go", "package dup\n\nvar a = 1\nvar b = 1\n")
	readFileFirst(t, ft, "dup.go")

	res := call(t, ft, ToolEditFile, map[string]any{
		"path":        "dup.go",
		"old_string":  "= 1",
		"new_string":  "= 2",
		"replace_all": true,
	})
	if res.IsError {
		t.Fatalf("replace_all should accept multiple matches: %s", res.Content)
	}
	b, _ := os.ReadFile(filepath.Join(ft.Root, "dup.go"))
	if strings.Count(string(b), "= 2") != 2 {
		t.Errorf("both occurrences should have been replaced: %q", b)
	}
	if !strings.Contains(res.Content, "2 occurrences") {
		t.Errorf("the result should report how many it changed, got %q", res.Content)
	}
}

func TestEditRefusesMissingOldString(t *testing.T) {
	ft, _ := newTools(t)
	readFileFirst(t, ft, "main.go")

	res := call(t, ft, ToolEditFile, map[string]any{
		"path":       "main.go",
		"old_string": "func nonexistent()",
		"new_string": "x",
	})
	if !res.IsError {
		t.Fatal("an old_string that is not present must be an error, not a silent no-op")
	}
	if !strings.Contains(res.Content, "byte for byte") {
		t.Errorf("error should explain that matching is exact, got %q", res.Content)
	}
}

func TestEditRequiresReadingTheFileFirst(t *testing.T) {
	ft, _ := newTools(t)

	// No read: the model would be editing from memory.
	res := call(t, ft, ToolEditFile, map[string]any{
		"path":       "main.go",
		"old_string": "func main() {}",
		"new_string": "func main() { println(1) }",
	})
	if !res.IsError {
		t.Fatal("editing a file that has not been read must be refused")
	}
	if !strings.Contains(res.Content, "read") {
		t.Errorf("error should say to read it first, got %q", res.Content)
	}

	// After a read the same edit is allowed.
	readFileFirst(t, ft, "main.go")
	if res := call(t, ft, ToolEditFile, map[string]any{
		"path":       "main.go",
		"old_string": "func main() {}",
		"new_string": "func main() { println(1) }",
	}); res.IsError {
		t.Fatalf("edit after a read should succeed: %s", res.Content)
	}
}

func TestEditRefusesNoOpAndEmpty(t *testing.T) {
	ft, _ := newTools(t)
	readFileFirst(t, ft, "main.go")

	if res := call(t, ft, ToolEditFile, map[string]any{
		"path": "main.go", "old_string": "func main() {}", "new_string": "func main() {}",
	}); !res.IsError {
		t.Error("an edit whose replacement is identical must be refused")
	}
	if res := call(t, ft, ToolEditFile, map[string]any{
		"path": "main.go", "old_string": "", "new_string": "x",
	}); !res.IsError {
		t.Error("an empty old_string must be refused")
	}
}

// Reset ends the round, and with it the conversation. A file read in a previous
// round was never seen by the model now holding the conversation.
func TestResetClearsTheReadSet(t *testing.T) {
	ft, _ := newTools(t)
	readFileFirst(t, ft, "main.go")
	ft.Reset()

	res := call(t, ft, ToolEditFile, map[string]any{
		"path": "main.go", "old_string": "func main() {}", "new_string": "func main() { println(1) }",
	})
	if !res.IsError {
		t.Fatal("a new round must require the file to be read again")
	}
}

func TestEditCannotEscapeTheWorktree(t *testing.T) {
	ft, _ := newTools(t)
	for _, p := range []string{"../outside.go", ".git/config"} {
		res := call(t, ft, ToolEditFile, map[string]any{
			"path": p, "old_string": "a", "new_string": "b",
		})
		if !res.IsError {
			t.Errorf("edit of %q should have been refused", p)
		}
	}
}
