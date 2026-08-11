package session

import (
	"fmt"
	"strings"

	"github.com/pmezard/go-difflib/difflib"
)

// What an edit changed, carried in the tool's own result.
//
// The dashboard used to reconstruct this from the call's arguments, and could
// not: inputCap bounds a recorded argument at 600 bytes, and a real edit's
// old_string and new_string are far longer than that together. What reached
// the browser was JSON cut off mid-string — unparseable, with both sides
// incomplete. No amount of client-side cleverness recovers content that was
// never sent.
//
// So the change travels in the result, which the tool controls and which the
// timeline already displays. The diff is computed over the whole file before
// and after, not over the two strings, so its line numbers are the file's own.
//
// difflib is used rather than a hand-rolled comparison: it produces standard
// unified output that the dashboard parses with an equally standard library on
// its side, and it is already in this module's dependency graph, so nothing
// new is being trusted.

// editDiffCap bounds the diff so it survives the event log intact.
//
// Detail is TAIL-truncated at detailCap, so a diff that overran would lose its
// head — the @@ hunk header included, which is the part a parser needs. Bounded
// here instead, well under the cap, leaving room for the "edited <path>" line
// above it.
const editDiffCap = 1400

// unifiedEdit renders the change to one file as a unified diff, or "" when
// nothing changed.
func unifiedEdit(rel, before, after string) string {
	if before == after {
		return ""
	}

	out, err := difflib.GetUnifiedDiffString(difflib.UnifiedDiff{
		A:        difflib.SplitLines(before),
		B:        difflib.SplitLines(after),
		FromFile: "a/" + rel,
		ToFile:   "b/" + rel,
		Context:  3,
	})
	if err != nil || strings.TrimSpace(out) == "" {
		return ""
	}
	return boundDiff(out)
}

// boundDiff trims a diff to editDiffCap at a line boundary.
//
// Cutting mid-line renders as a corrupt row in a viewer that is drawing one
// element per line, and a diff that is quietly incomplete is worse than one
// that says so — a reader would otherwise take the visible part for the whole
// change.
func boundDiff(diff string) string {
	if len(diff) <= editDiffCap {
		return diff
	}

	// The notice is part of the budget, not an addition to it: appending it
	// after trimming to the cap is how a bound gets quietly exceeded.
	const notice = "\n... (diff truncated)\n"
	cut := diff[:editDiffCap-len(notice)]
	if i := strings.LastIndexByte(cut, '\n'); i > 0 {
		cut = cut[:i]
	}
	return cut + notice
}

// editSummary is the tool result for an edit: what was changed, then how.
func editSummary(rel string, occurrences int, diff string) string {
	head := "edited " + rel
	if occurrences > 1 {
		head = fmt.Sprintf("edited %s (%d occurrences replaced)", rel, occurrences)
	}
	if diff == "" {
		return head
	}
	return head + "\n" + diff
}
