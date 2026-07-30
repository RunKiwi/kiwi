package loop

import (
	"encoding/json"
	"strings"
	"testing"
)

// The production failure: a model pasted file content into the JSON string
// verbatim, so the response carried literal newlines and died with
// "invalid character '\n' in string literal" — losing an otherwise good edit.
func TestEscapeControlCharsInStringsRecoversRawNewlines(t *testing.T) {
	raw := "{\"files\":[{\"path\":\"a.js\",\"content\":\"line1\nline2\n\"}]}"

	if json.Valid([]byte(raw)) {
		t.Fatal("precondition: the raw response should be invalid JSON")
	}

	repaired := escapeControlCharsInStrings(raw)

	var got struct {
		Files []struct {
			Path    string `json:"path"`
			Content string `json:"content"`
		} `json:"files"`
	}
	if err := json.Unmarshal([]byte(repaired), &got); err != nil {
		t.Fatalf("repaired JSON should parse: %v\nrepaired: %s", err, repaired)
	}
	if len(got.Files) != 1 {
		t.Fatalf("files: got %d, want 1", len(got.Files))
	}
	// The content must survive byte-for-byte — this is an encoding repair, not
	// a content rewrite. A file written with the newlines mangled would be a
	// worse outcome than the original parse error.
	if got.Files[0].Content != "line1\nline2\n" {
		t.Errorf("content = %q, want %q", got.Files[0].Content, "line1\nline2\n")
	}
	if got.Files[0].Path != "a.js" {
		t.Errorf("path = %q, want a.js", got.Files[0].Path)
	}
}

func TestEscapeControlCharsInStringsHandlesTabsAndReturns(t *testing.T) {
	raw := "{\"content\":\"if (x) {\n\treturn 1;\r\n}\"}"
	var got struct {
		Content string `json:"content"`
	}
	if err := json.Unmarshal([]byte(escapeControlCharsInStrings(raw)), &got); err != nil {
		t.Fatalf("repaired JSON should parse: %v", err)
	}
	if got.Content != "if (x) {\n\treturn 1;\r\n}" {
		t.Errorf("content = %q", got.Content)
	}
}

// Structural whitespace is not inside a string literal and must be left alone,
// or the repair would corrupt well-formed pretty-printed JSON.
func TestEscapeControlCharsInStringsLeavesValidJSONUnchanged(t *testing.T) {
	valid := "{\n  \"files\": [\n    {\"path\": \"a.js\", \"content\": \"ok\\n\"}\n  ]\n}"
	if got := escapeControlCharsInStrings(valid); got != valid {
		t.Errorf("valid JSON should be untouched:\n got: %q\nwant: %q", got, valid)
	}
}

// An already-escaped \" must not be read as the end of the string, or every
// subsequent newline would be treated as structural and left broken.
func TestEscapeControlCharsInStringsRespectsEscapedQuotes(t *testing.T) {
	raw := "{\"content\":\"say \\\"hi\\\"\nthen stop\"}"
	var got struct {
		Content string `json:"content"`
	}
	if err := json.Unmarshal([]byte(escapeControlCharsInStrings(raw)), &got); err != nil {
		t.Fatalf("repaired JSON should parse: %v", err)
	}
	if got.Content != "say \"hi\"\nthen stop" {
		t.Errorf("content = %q, want %q", got.Content, "say \"hi\"\nthen stop")
	}
}

// A trailing backslash must not make the scanner consume the closing quote.
func TestEscapeControlCharsInStringsHandlesEscapedBackslash(t *testing.T) {
	raw := "{\"content\":\"path\\\\\ntail\"}"
	var got struct {
		Content string `json:"content"`
	}
	if err := json.Unmarshal([]byte(escapeControlCharsInStrings(raw)), &got); err != nil {
		t.Fatalf("repaired JSON should parse: %v", err)
	}
	if got.Content != "path\\\ntail" {
		t.Errorf("content = %q", got.Content)
	}
}

// Unescaped quotes inside content are deliberately NOT repaired: guessing where
// the string ends would silently corrupt the file being written. Such input must
// still be returned in a state that fails loudly rather than parsing wrongly.
func TestEscapeControlCharsInStringsDoesNotGuessAtUnescapedQuotes(t *testing.T) {
	raw := "{\"content\":\"he said \"hi\" loudly\"}"
	repaired := escapeControlCharsInStrings(raw)
	var got map[string]string
	if err := json.Unmarshal([]byte(repaired), &got); err == nil {
		t.Errorf("ambiguous input must not silently parse, got %v", got)
	}
}

// Other control characters fall back to \uXXXX rather than being dropped.
func TestEscapeControlCharsInStringsEscapesExoticControls(t *testing.T) {
	raw := "{\"content\":\"a\x01b\"}"
	repaired := escapeControlCharsInStrings(raw)
	if !strings.Contains(repaired, `\u0001`) {
		t.Fatalf("expected \\u0001 escape, got %q", repaired)
	}
	var got map[string]string
	if err := json.Unmarshal([]byte(repaired), &got); err != nil {
		t.Fatalf("repaired JSON should parse: %v", err)
	}
	if got["content"] != "a\x01b" {
		t.Errorf("content = %q", got["content"])
	}
}

// A response cut off mid-answer must be diagnosed as truncation, not left as a
// bare "unexpected end of JSON input" that says nothing about what to do.
func TestTruncationHintDetectsCutOffResponse(t *testing.T) {
	truncated := `{"files":[{"path":"a.js","content":"function x() {`
	got := truncationHint(truncated, "")
	if got == "" {
		t.Fatal("unbalanced braces should be reported as a likely truncation")
	}
	if !strings.Contains(got, "cut off") {
		t.Errorf("hint should say the response was cut off, got %q", got)
	}
}

// A well-formed document must not be blamed on truncation — that would send
// someone chasing a token limit over what is really a different bug.
func TestTruncationHintSilentOnBalancedJSON(t *testing.T) {
	balanced := `{"files":[{"path":"a.js","content":"ok"}]}`
	if got := truncationHint(balanced, ""); got != "" {
		t.Errorf("balanced JSON should produce no hint, got %q", got)
	}
}
