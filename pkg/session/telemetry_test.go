package session

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// Every phase of a session emitted a duration except the one that takes the
// longest: the Implementer's own model turn. The sum of a task's events
// therefore came to less than its wall clock, and the difference — generation
// time, plus any provider backoff inside it — was attributed to nothing. These
// tests pin the two facts that made "where did the time go" answerable.

func eventsWithPhase(events []Event, phase string) []Event {
	var out []Event
	for _, e := range events {
		if e.Phase == phase {
			out = append(out, e)
		}
	}
	return out
}

func TestImplementerTurnIsReported(t *testing.T) {
	arch := &fakeArchitect{
		plan:    Spec{Verdict: VerdictProceed, Objective: "do the thing"},
		reviews: []Spec{{Verdict: VerdictApprove, Summary: "done"}},
	}
	ws := &fakeWorkspace{tree: []string{"main.go"}, diff: "+x", files: []string{"main.go"}, head: "base"}
	r, events := newRunner(t, arch, finishingRunner("did it"), ws, passing("ok"))

	if _, err := r.Run(context.Background(), Task{Description: "x", TestCmd: "go test ./..."}); err != nil {
		t.Fatalf("Run: %v", err)
	}

	turns := eventsWithPhase(*events, "implementer")
	if len(turns) == 0 {
		t.Fatal("the Implementer's turn must be reported like every other phase")
	}
	// A turn that asked for tools is not a failure and must not be coloured as
	// one; the outcome distinguishes it from a turn that ended.
	if turns[0].Outcome != "tools" && turns[0].Outcome != "done" {
		t.Errorf("unexpected turn outcome %q", turns[0].Outcome)
	}
}

func TestToolEventsCarryTheirArguments(t *testing.T) {
	arch := &fakeArchitect{
		plan:    Spec{Verdict: VerdictProceed, Objective: "do the thing"},
		reviews: []Spec{{Verdict: VerdictApprove, Summary: "done"}},
	}
	ws := &fakeWorkspace{tree: []string{"main.go"}, diff: "+x", files: []string{"main.go"}, head: "base"}
	r, events := newRunner(t, arch, finishingRunner("did it"), ws, passing("ok"))

	if _, err := r.Run(context.Background(), Task{Description: "x", TestCmd: "go test ./..."}); err != nil {
		t.Fatalf("Run: %v", err)
	}

	tools := eventsWithPhase(*events, "tool")
	if len(tools) == 0 {
		t.Fatal("expected at least one tool event")
	}
	// The name and the output alone could never answer "what did it actually
	// run" — which is exactly the question a reader has about a `run` call.
	var found bool
	for _, e := range tools {
		if e.Tool != ToolFinish || e.Input == "" {
			continue
		}
		var args map[string]any
		if err := json.Unmarshal([]byte(e.Input), &args); err != nil {
			t.Fatalf("Input should be the arguments as JSON, got %q: %v", e.Input, err)
		}
		if args["note"] != "did it" {
			t.Errorf("arguments did not survive: %v", args)
		}
		found = true
	}
	if !found {
		t.Errorf("no tool event carried its arguments: %+v", tools)
	}
}

// Arguments are capped from the front, unlike output. A tool call says what it
// is at its start — the path, the pattern, the command — where command output
// explains itself at its end.
func TestInputIsCappedFromTheFront(t *testing.T) {
	r := &Runner{}
	var got Event
	r.Config.OnEvent = func(e Event) { got = e }

	long := "START" + strings.Repeat("x", inputCap*2)
	r.emit(&state{}, Event{Phase: "tool", Input: long})

	if len(got.Input) > inputCap {
		t.Errorf("input not capped: %d bytes", len(got.Input))
	}
	if !strings.HasPrefix(got.Input, "START") {
		t.Errorf("the front of the argument must survive, got %q", head(got.Input, 20))
	}
}

func TestHeadTrimsToRuneBoundary(t *testing.T) {
	// Two-byte runes across the cut: the result must stay valid UTF-8 rather
	// than ending in half a character.
	s := strings.Repeat("é", 10)
	for n := 1; n <= len(s); n++ {
		got := head(s, n)
		if !utf8Valid(got) {
			t.Fatalf("head(%d) produced invalid UTF-8: %q", n, got)
		}
	}
}

func utf8Valid(s string) bool {
	for _, r := range s {
		if r == '�' {
			return false
		}
	}
	return true
}
