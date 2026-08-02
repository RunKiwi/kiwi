package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// Gemini attaches an opaque thoughtSignature to functionCall parts and rejects
// the next request if the replayed turn does not carry it back:
//
//	400 INVALID_ARGUMENT: Function call is missing a thought_signature in
//	functionCall parts.
//
// This is a regression test for a live-only failure. Every mock in the suite
// echoes back whatever it is handed, so the tool loop passed its tests and then
// failed on the second turn of the first real Gemini session in production.
func TestGeminiEchoesThoughtSignatureOnReplay(t *testing.T) {
	var bodies []map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		bodies = append(bodies, body)

		if len(bodies) == 1 {
			_, _ = w.Write([]byte(`{"candidates":[{"content":{"role":"model","parts":[
				{"functionCall":{"name":"read_file","args":{"path":"a.go"}},
				 "thoughtSignature":"SIG-ABC-123"}]},
				"finishReason":"STOP"}],"usageMetadata":{}}`))
			return
		}
		_, _ = w.Write([]byte(`{"candidates":[{"content":{"role":"model","parts":[{"text":"done"}]},"finishReason":"STOP"}],"usageMetadata":{}}`))
	}))
	defer srv.Close()

	p := NewGeminiProviderWithModels("k", "gemini-flash-latest", "gemini-flash-latest")
	p.baseURL = srv.URL
	conv := p.StartConversation("s", toolDefs(), ConversationOpts{})

	first, err := conv.Send(context.Background(), "go", nil)
	if err != nil {
		t.Fatalf("first send: %v", err)
	}
	if len(first.Calls) != 1 {
		t.Fatalf("expected one tool call, got %d", len(first.Calls))
	}

	if _, err := conv.Send(context.Background(), "", []ToolResult{
		{CallID: first.Calls[0].ID, Content: "package main"},
	}); err != nil {
		t.Fatalf("second send: %v", err)
	}

	// The second request must replay the model turn WITH its signature intact.
	contents, _ := bodies[1]["contents"].([]any)
	var found string
	for _, c := range contents {
		parts, _ := c.(map[string]any)["parts"].([]any)
		for _, p := range parts {
			pm, _ := p.(map[string]any)
			if pm["functionCall"] == nil {
				continue
			}
			if sig, ok := pm["thoughtSignature"].(string); ok {
				found = sig
			}
		}
	}
	if found != "SIG-ABC-123" {
		t.Errorf("replayed functionCall carried thoughtSignature %q, want %q — Gemini rejects the turn without it", found, "SIG-ABC-123")
	}
}

// A part with no signature must not emit an empty one; Gemini is strict about
// the shape of parts it did not send.
func TestGeminiOmitsAbsentThoughtSignature(t *testing.T) {
	b, err := json.Marshal(geminiToolPart{Text: "hi"})
	if err != nil {
		t.Fatal(err)
	}
	if got := string(b); got != `{"text":"hi"}` {
		t.Errorf("marshalled %s, want no thoughtSignature key", got)
	}
}
