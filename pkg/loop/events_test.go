package loop

import (
	"context"
	"strings"
	"testing"
	"unicode/utf8"
)

// The daemon runs this loop in its own process, so an Actor edit or a Critic
// verdict that is not emitted here is not observed anywhere. These tests pin
// the event stream that becomes an execution record's evidence.

func phases(evs []Event) []string {
	out := make([]string, 0, len(evs))
	for _, e := range evs {
		out = append(out, e.Phase+"/"+e.Outcome)
	}
	return out
}

func TestEventsCoverTheWholeLoop(t *testing.T) {
	path := writeTemp(t, "broken")
	prov := &scriptedProvider{edits: []string{"fixed"}}

	var got []Event
	r := &Runner{Provider: prov, Config: Config{OnEvent: func(e Event) { got = append(got, e) }}}

	if _, err := r.Run(context.Background(), Task{Description: "fix", FilePath: path},
		passWhenContains(path, "fixed")); err != nil {
		t.Fatal(err)
	}

	want := []string{"initial_test/fail", "actor/proposed", "test/pass"}
	if strings.Join(phases(got), ",") != strings.Join(want, ",") {
		t.Errorf("phases = %v, want %v", phases(got), want)
	}
	if got[0].Step != 0 {
		t.Errorf("initial test step = %d, want 0", got[0].Step)
	}
	if got[1].Step != 1 {
		t.Errorf("first actor step = %d, want 1", got[1].Step)
	}
}

// A rejection is the signal the record exists to carry: it is the evidence that
// something reviewed the edit before the test ever ran.
func TestRejectedEditIsEmittedWithReasons(t *testing.T) {
	path := writeTemp(t, "broken")
	prov := &scriptedProvider{edits: []string{"fixed"}}
	critic := &scriptedCritic{approve: []bool{false, true}}

	var got []Event
	r := &Runner{Provider: prov, Critic: critic,
		Config: Config{OnEvent: func(e Event) { got = append(got, e) }}}

	if _, err := r.Run(context.Background(), Task{Description: "fix", FilePath: path},
		passWhenContains(path, "fixed")); err != nil {
		t.Fatal(err)
	}

	var rejected, approved int
	for _, e := range got {
		if e.Phase != "critic" {
			continue
		}
		switch e.Outcome {
		case "rejected":
			rejected++
		case "approved":
			approved++
		}
	}
	if rejected != 1 {
		t.Errorf("rejected critic events = %d, want 1", rejected)
	}
	if approved != 1 {
		t.Errorf("approved critic events = %d, want 1", approved)
	}

	// A rejected edit must never reach the test command.
	for i, e := range got {
		if e.Phase == "critic" && e.Outcome == "rejected" {
			if i+1 < len(got) && got[i+1].Phase == "test" {
				t.Error("a test ran immediately after a rejected edit")
			}
		}
	}
}

// A nil OnEvent must cost nothing and change nothing.
func TestNilOnEventIsSafe(t *testing.T) {
	path := writeTemp(t, "broken")
	prov := &scriptedProvider{edits: []string{"fixed"}}
	r := &Runner{Provider: prov}

	res, err := r.Run(context.Background(), Task{Description: "fix", FilePath: path},
		passWhenContains(path, "fixed"))
	if err != nil {
		t.Fatal(err)
	}
	if !res.Success {
		t.Error("loop should still succeed with no event sink")
	}
}

// Detail is capped on a rune boundary: the daemon forwards it over the wire and
// into a signed attestation, where invalid UTF-8 would not round-trip.
func TestDetailIsCappedOnRuneBoundary(t *testing.T) {
	long := strings.Repeat("é", detailCap)
	got := tailOf(long, detailCap)
	if len(got) > detailCap {
		t.Errorf("length %d exceeds cap %d", len(got), detailCap)
	}
	if !utf8.ValidString(got) {
		t.Error("truncation split a rune")
	}
}
