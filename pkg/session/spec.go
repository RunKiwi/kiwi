package session

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// Verdicts the Architect may return.
const (
	// VerdictProceed opens a round: the spec describes work to do.
	VerdictProceed = "proceed"
	// VerdictRevise rejects what a round produced and describes what to change.
	// It is distinct from proceed only in what it says about the last round;
	// both open the next one.
	VerdictRevise = "revise"
	// VerdictApprove ends the session successfully.
	VerdictApprove = "approve"
	// VerdictAbandon ends it unsuccessfully: the Architect judges the task
	// impossible or misconceived, and says why. This is a real outcome, not a
	// failure to reach one — a task asking for something the repository cannot
	// support should stop early and explain, not burn four rounds proving it.
	VerdictAbandon = "abandon"
)

// Spec is the Architect's instruction for one round.
//
// It replaces the Critic's {approved, reasons} pair, and the difference is the
// point. Today a rejection is a string concatenated onto the next Actor prompt
// inside the build-output argument, because the provider signature has nowhere
// else to put it — so the next attempt receives a complaint, not a brief, and
// has no memory that the previous round happened. A Spec is the round's input:
// it states the objective, what "done" means, and what the Implementer may and
// may not touch.
type Spec struct {
	Verdict   string `json:"verdict"`
	Rationale string `json:"rationale"`
	Objective string `json:"objective"`
	// AcceptanceCriteria is what the Architect will check on review. Stating it
	// up front is what lets a round be judged against something other than the
	// reviewer's mood on the day.
	AcceptanceCriteria []string `json:"acceptance_criteria"`
	// MustChange and MustNotChange are advisory to the Implementer and binding
	// on the review. MustNotChange is where the anti-gaming rule lives now: the
	// single-file loop refuses outright to edit a test while tests are red,
	// which is correct but blunt — it also rejects "add tests for the parser".
	// An Architect that has seen the repository can name the specific files
	// whose weakening would fake the fix, and leave the rest alone.
	MustChange    []string `json:"must_change"`
	MustNotChange []string `json:"must_not_change"`
	Hints         []string `json:"hints"`
	OpenQuestions []string `json:"open_questions"`
	// Summary is the Architect's account of the finished work, filled in on the
	// approving review and used as the pull request body. The same context that
	// planned the task writes its description, which is the whole reason the
	// role is persistent.
	Summary string `json:"summary"`
}

// Report is the Implementer's return value from a round, via the finish tool.
// It is the answer half of the channel Spec opens: OpenQuestions goes out with
// the brief, and this is what comes back — instead of both directions being
// flattened into one paragraph of prose the Architect has to parse to tell
// whether a question actually got answered.
type Report struct {
	// Note is the free-text handoff: what changed, what to know. Always present.
	Note string
	// Answers responds to this round's Spec.OpenQuestions, in the same order.
	// Not enforced positionally — the Architect reads both lists and matches
	// them the way a person would — but structured rather than buried in Note
	// is what makes "was this actually answered" checkable instead of assumed.
	Answers []string
	// NewQuestions are things this round could not resolve on its own. Before
	// this field existed, an Implementer that hit something undecidable had no
	// way to say so — it could only guess and hope the guess survived review,
	// or bury a hedge in Note where nothing downstream looked for it.
	NewQuestions []string
	// Decisions are implementation choices worth the reviewer knowing about
	// even though nothing forced the question — e.g. "used the existing fs
	// backend since pkg/store owns the interface it expects."
	Decisions []string
}

// Opens reports whether this verdict starts another round.
func (s Spec) Opens() bool { return s.Verdict == VerdictProceed || s.Verdict == VerdictRevise }

// Terminal reports whether this verdict ends the session.
func (s Spec) Terminal() bool { return s.Verdict == VerdictApprove || s.Verdict == VerdictAbandon }

// fingerprint identifies what a spec is asking for, so two consecutive rounds
// asking for the same thing can be recognised as a reviewer that is looping.
//
// It hashes the objective and the sorted MustChange set, and deliberately
// ignores rationale and hints: a reviewer rephrasing the same demand is looping
// just as surely as one repeating it verbatim, and those two fields are where
// rephrasing shows up. The single-file loop's rejectionHalt comment records
// exactly this failure — three correct, identical rejections the Actor could
// not satisfy — caught there by counting rejections rather than comparing them.
func (s Spec) fingerprint() string {
	files := append([]string(nil), s.MustChange...)
	sort.Strings(files)
	h := sha256.New()
	fmt.Fprintf(h, "%s\x00%s", strings.ToLower(strings.TrimSpace(s.Objective)), strings.Join(files, "\x00"))
	return hex.EncodeToString(h.Sum(nil))
}

// Prompt renders the spec as the Implementer's brief.
func (s Spec) Prompt() string {
	var b strings.Builder
	b.WriteString("## Objective\n")
	b.WriteString(s.Objective)
	b.WriteString("\n")

	if s.Rationale != "" {
		b.WriteString("\n## Why this round exists\n")
		b.WriteString(s.Rationale)
		b.WriteString("\n")
	}
	writeList(&b, "Acceptance criteria — the reviewer will check these", s.AcceptanceCriteria)
	writeList(&b, "Files expected to change", s.MustChange)
	writeList(&b, "Files you must NOT change", s.MustNotChange)
	writeList(&b, "Hints", s.Hints)
	writeList(&b, "Open questions — call finish with one entry per question in `answers`, in this order; put anything you cannot resolve in `new_questions` instead of guessing", s.OpenQuestions)
	return b.String()
}

func writeList(b *strings.Builder, title string, items []string) {
	if len(items) == 0 {
		return
	}
	fmt.Fprintf(b, "\n## %s\n", title)
	for _, it := range items {
		fmt.Fprintf(b, "- %s\n", it)
	}
}

// parseSpec extracts a Spec from a model response.
//
// It reuses the tolerance the single-file loop had to learn the hard way:
// models wrap JSON in prose or a fenced block, so the object is located by its
// outermost braces rather than by assuming the whole response is JSON.
func parseSpec(resp string) (Spec, error) {
	start := strings.IndexByte(resp, '{')
	end := strings.LastIndexByte(resp, '}')
	if start == -1 || end == -1 || start >= end {
		return Spec{}, fmt.Errorf("no JSON object in architect response")
	}
	var s Spec
	if err := json.Unmarshal([]byte(resp[start:end+1]), &s); err != nil {
		return Spec{}, fmt.Errorf("parse architect response: %w", err)
	}

	s.Verdict = strings.ToLower(strings.TrimSpace(s.Verdict))
	switch s.Verdict {
	case VerdictProceed, VerdictRevise, VerdictApprove, VerdictAbandon:
	case "":
		return Spec{}, fmt.Errorf("architect response has no verdict")
	default:
		return Spec{}, fmt.Errorf("architect returned unknown verdict %q", s.Verdict)
	}
	if s.Opens() && strings.TrimSpace(s.Objective) == "" {
		return Spec{}, fmt.Errorf("architect verdict %q carries no objective", s.Verdict)
	}
	return s, nil
}
