package session

import (
	"fmt"
	"strings"
	"testing"
)

// A large file used to be truncated from the FRONT — cap() keeps the tail,
// which is right for a failing build and wrong for source code, where the
// package clause, the imports and the type declarations are all at the top. A
// model that got the bottom of a file and was then asked by write_file for "the
// complete new contents" had to invent the part it never saw.

func bigFile(lines int) string {
	var b strings.Builder
	b.WriteString("package big\n")
	for i := 2; i <= lines; i++ {
		fmt.Fprintf(&b, "// line %d\n", i)
	}
	return b.String()
}

func TestReadKeepsTheTopAndSaysWhatItDropped(t *testing.T) {
	ft, _ := newTools(t)
	total := defaultReadLines + 500
	mustWrite(t, ft.Root, "big.go", bigFile(total))

	res := call(t, ft, ToolReadFile, map[string]string{"path": "big.go"})
	if res.IsError {
		t.Fatalf("read failed: %s", res.Content)
	}
	if !strings.Contains(res.Content, "package big") {
		t.Error("the top of the file must survive truncation — it is what the model needs to rewrite it")
	}
	if strings.Contains(res.Content, fmt.Sprintf("// line %d\n", total)) {
		t.Error("the tail should have been dropped, not the head")
	}
	// A truncation the model cannot see is how a partial read becomes a
	// confident whole-file rewrite of a file it only half read.
	if !strings.Contains(res.Content, "read further with offset") {
		t.Errorf("truncation must be announced, got tail %q", tail(res.Content, 200))
	}
	if !strings.Contains(res.Content, fmt.Sprintf("of %d", total)) {
		t.Error("the notice should state the file's real length")
	}
}

func TestReadNumbersLinesAndHonoursOffsetLimit(t *testing.T) {
	ft, _ := newTools(t)
	mustWrite(t, ft.Root, "big.go", bigFile(50))

	res := call(t, ft, ToolReadFile, map[string]any{"path": "big.go", "offset": 10, "limit": 3})
	if res.IsError {
		t.Fatalf("read failed: %s", res.Content)
	}
	for _, want := range []string{"10\t// line 10", "11\t// line 11", "12\t// line 12"} {
		if !strings.Contains(res.Content, want) {
			t.Errorf("missing %q in:\n%s", want, res.Content)
		}
	}
	if strings.Contains(res.Content, "13\t") {
		t.Errorf("limit was not honoured:\n%s", res.Content)
	}
	if strings.Contains(res.Content, "package big") {
		t.Errorf("offset was not honoured:\n%s", res.Content)
	}
}

func TestReadPastTheEndSaysSo(t *testing.T) {
	ft, _ := newTools(t)
	mustWrite(t, ft.Root, "small.go", "package small\n")

	res := call(t, ft, ToolReadFile, map[string]any{"path": "small.go", "offset": 99})
	if res.IsError {
		t.Fatalf("an out-of-range offset is ordinary feedback, not a failure: %s", res.Content)
	}
	if !strings.Contains(res.Content, "past the end") {
		t.Errorf("should explain the offset is past the end, got %q", res.Content)
	}
}

func TestGrepContextShowsSurroundingLines(t *testing.T) {
	ft, _ := newTools(t)
	mustWrite(t, ft.Root, "ctx.go", "package ctx\n\nfunc before() {}\nfunc target() {}\nfunc after() {}\n")

	res := call(t, ft, ToolGrep, map[string]any{"pattern": "func target", "context": 1})
	if res.IsError {
		t.Fatalf("grep failed: %s", res.Content)
	}
	// Context is the whole point: without it every match cost a follow-up
	// read_file of the entire surrounding file.
	if !strings.Contains(res.Content, "func before") || !strings.Contains(res.Content, "func after") {
		t.Errorf("context lines missing:\n%s", res.Content)
	}
	// The matching line keeps ':' and context uses '-', so the match stays
	// findable by eye — the convention grep itself uses.
	if !strings.Contains(res.Content, "ctx.go:4:func target() {}") {
		t.Errorf("the matching line should be marked with ':':\n%s", res.Content)
	}
	if !strings.Contains(res.Content, "ctx.go-3-func before() {}") {
		t.Errorf("context lines should be marked with '-':\n%s", res.Content)
	}
}

func TestGrepGlobRestrictsFiles(t *testing.T) {
	ft, _ := newTools(t)
	mustWrite(t, ft.Root, "a.go", "// needle\n")
	mustWrite(t, ft.Root, "b.txt", "// needle\n")

	res := call(t, ft, ToolGrep, map[string]any{"pattern": "needle", "glob": "*.go"})
	if res.IsError {
		t.Fatalf("grep failed: %s", res.Content)
	}
	if !strings.Contains(res.Content, "a.go") {
		t.Errorf("the matching .go file should be reported:\n%s", res.Content)
	}
	if strings.Contains(res.Content, "b.txt") {
		t.Errorf("the glob should have excluded b.txt:\n%s", res.Content)
	}
}

// "(no matches)" for a malformed glob is indistinguishable from a genuine miss,
// which is the worst possible answer: the model concludes the code is not there.
func TestGrepRejectsMalformedGlob(t *testing.T) {
	ft, _ := newTools(t)
	res := call(t, ft, ToolGrep, map[string]any{"pattern": "x", "glob": "[unclosed"})
	if !res.IsError {
		t.Fatal("a malformed glob must be reported rather than silently matching nothing")
	}
}

func TestGrepNoMatchNamesThePattern(t *testing.T) {
	ft, _ := newTools(t)
	res := call(t, ft, ToolGrep, map[string]any{"pattern": "definitely_not_here"})
	if res.IsError {
		t.Fatalf("no matches is not an error: %s", res.Content)
	}
	if !strings.Contains(res.Content, "definitely_not_here") {
		t.Errorf("the message should name what was searched for, got %q", res.Content)
	}
}
