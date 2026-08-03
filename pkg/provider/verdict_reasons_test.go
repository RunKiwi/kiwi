package provider

import "testing"

// The Critic is asked for `reasons` as a string and routinely answers with a
// list — one entry per objection, which is a fair reading of the plural.
// Against a plain string field that is a hard unmarshal error, and parseVerdict
// fails safe by counting any parse error as a rejection. So a Critic that was
// only formatting differently produced three rejections in a row and failed the
// task, surfacing a Go type error instead of a review:
//
//	loop failed after 3 step(s): the Critic rejected 3 attempts in a row —
//	last reason: could not parse critic verdict: json: cannot unmarshal array
//	into Go struct field Verdict.reasons of type string
//
// Observed in production on claude-haiku-4-5.
func TestParseVerdictAcceptsReasonsAsAList(t *testing.T) {
	v := parseVerdict(`{"approved": false, "reasons": ["missing a test", "wrong error type"]}`)

	if v.Reasons == "" {
		t.Fatal("a list of reasons was dropped entirely")
	}
	// Every objection has to survive: the Actor needs all of them to fix the
	// edit in one pass, so joining beats taking the first.
	for _, want := range []string{"missing a test", "wrong error type"} {
		if !contains(v.Reasons, want) {
			t.Errorf("reasons %q lost %q", v.Reasons, want)
		}
	}
	if contains(v.Reasons, "cannot unmarshal") {
		t.Errorf("a well-formed verdict was reported as a parse failure: %q", v.Reasons)
	}
}

// The approval bit must survive the array case too — this is the one that
// decides whether work ships.
func TestParseVerdictKeepsApprovalWithListReasons(t *testing.T) {
	v := parseVerdict(`{"approved": true, "reasons": ["nit: naming"]}`)
	if !v.Approved {
		t.Error("approved:true was lost when reasons came back as a list")
	}
}

func TestParseVerdictStillAcceptsAPlainString(t *testing.T) {
	v := parseVerdict(`{"approved": true, "reasons": "looks correct"}`)
	if !v.Approved || v.Reasons != "looks correct" {
		t.Errorf("verdict = %+v", v)
	}
}

// Some models answer with a list of objects rather than of strings.
func TestParseVerdictFlattensObjectReasons(t *testing.T) {
	v := parseVerdict(`{"approved": false, "reasons": [{"reason": "no bounds check"}, {"message": "typo"}]}`)
	for _, want := range []string{"no bounds check", "typo"} {
		if !contains(v.Reasons, want) {
			t.Errorf("reasons %q lost %q", v.Reasons, want)
		}
	}
}

// An unrecognised shape keeps the raw JSON rather than discarding it — whoever
// reads the task detail is better served by the Critic's actual words.
func TestParseVerdictKeepsUnknownShapesVerbatim(t *testing.T) {
	v := parseVerdict(`{"approved": false, "reasons": {"a": 1}}`)
	if v.Reasons == "" {
		t.Error("an unrecognised reasons shape was dropped instead of preserved")
	}
}

// Absent reasons are legal and must not read as a parse failure.
func TestParseVerdictToleratesMissingReasons(t *testing.T) {
	v := parseVerdict(`{"approved": true}`)
	if !v.Approved {
		t.Error("approved:true lost when reasons was absent")
	}
	if contains(v.Reasons, "could not parse") {
		t.Errorf("absent reasons reported as a parse failure: %q", v.Reasons)
	}
}

// Genuinely malformed input must still fail safe as a rejection.
func TestParseVerdictStillRejectsGarbage(t *testing.T) {
	v := parseVerdict("the critic said nothing useful")
	if v.Approved {
		t.Error("unparseable output must not be treated as approval")
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && func() bool {
		for i := 0; i+len(needle) <= len(haystack); i++ {
			if haystack[i:i+len(needle)] == needle {
				return true
			}
		}
		return false
	}()
}
