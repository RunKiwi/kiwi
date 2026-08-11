package session

import (
	"path/filepath"
	"strings"
	"testing"
)

// An edit's arguments are capped at 600 bytes on the way into the event log
// (inputCap), so a real edit arrives at the dashboard as JSON cut off
// mid-string — unparseable, and with the before and after both incomplete.
// The change therefore has to travel in the result, which the tool controls,
// rather than being reconstructed from arguments that were never sent whole.
func TestEditDiffShowsWhatChanged(t *testing.T) {
	before := "package main\n\nfunc main() {\n\tprintln(1)\n}\n"
	after := "package main\n\nfunc main() {\n\tprintln(2)\n}\n"

	diff := unifiedEdit("main.go", before, after)

	if !strings.Contains(diff, "@@") {
		t.Errorf("expected a unified diff hunk header, got:\n%s", diff)
	}
	if !strings.Contains(diff, "-\tprintln(1)") {
		t.Errorf("the removed line should be marked, got:\n%s", diff)
	}
	if !strings.Contains(diff, "+\tprintln(2)") {
		t.Errorf("the added line should be marked, got:\n%s", diff)
	}
	// Context, so the change has somewhere to sit.
	if !strings.Contains(diff, " func main() {") {
		t.Errorf("expected surrounding context, got:\n%s", diff)
	}
}

// Detail is tail-truncated at detailCap on the way into the event log, so a
// diff that runs past it loses its head — the @@ header included, which is the
// part a parser needs. Bounding it here keeps the whole thing parseable.
func TestEditDiffIsBoundedWellUnderTheDetailCap(t *testing.T) {
	var before, after strings.Builder
	for i := 0; i < 400; i++ {
		before.WriteString("line ")
		before.WriteString(strings.Repeat("x", 20))
		before.WriteString("\n")
		after.WriteString("CHANGED ")
		after.WriteString(strings.Repeat("x", 20))
		after.WriteString("\n")
	}

	diff := unifiedEdit("big.go", before.String(), after.String())

	if len(diff) > editDiffCap {
		t.Errorf("diff is %d bytes, want at most %d", len(diff), editDiffCap)
	}
	if len(diff)+len("edited big.go\n") >= detailCap {
		t.Errorf("diff plus its heading is %d bytes, which detailCap (%d) would truncate",
			len(diff)+len("edited big.go\n"), detailCap)
	}
	if !strings.Contains(diff, "@@") {
		t.Error("a truncated diff must still start with a hunk a parser can read")
	}
	if !strings.Contains(diff, "truncated") {
		t.Error("a truncated diff must say so rather than looking complete")
	}
	// Trimmed at a line boundary: half a line renders as a corrupt diff row.
	if strings.HasSuffix(diff, "x") {
		t.Error("the diff was cut mid-line")
	}
}

// A no-op edit has nothing to draw, and an empty hunk would render as an empty
// black box in the timeline.
func TestEditDiffIsEmptyWhenNothingChanged(t *testing.T) {
	if diff := unifiedEdit("main.go", "same\n", "same\n"); diff != "" {
		t.Errorf("got %q, want empty", diff)
	}
}

// The tool result is what the dashboard reads, so the diff has to be in it.
func TestEditResultCarriesTheDiff(t *testing.T) {
	dir := t.TempDir()
	tools := &FileTools{Root: dir}
	if err := tools.write("main.go", "package main\n\nfunc main() {\n\tprintln(1)\n}\n"); err != nil {
		t.Fatal(err)
	}

	// edit_file refuses to change a file it has not seen this round, which is
	// the precondition the model has to satisfy too. The read set is keyed by
	// resolved path, as markRead records it.
	tools.markRead(filepath.Join(dir, "main.go"))

	res, err := tools.edit("main.go", "println(1)", "println(2)", false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(res, "edited main.go") {
		t.Errorf("the result should still say what it edited, got: %q", res)
	}
	if !strings.Contains(res, "@@") || !strings.Contains(res, "+\tprintln(2)") {
		t.Errorf("the result should carry the diff, got:\n%s", res)
	}
}
