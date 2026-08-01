package provider

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func toolDefs() []ToolDef {
	return []ToolDef{
		{Name: "read_file", Description: "Read a file", Properties: map[string]any{"path": map[string]any{"type": "string"}}, Required: []string{"path"}},
		{Name: "finish", Description: "Finish", Properties: map[string]any{}},
	}
}

// Every provider Kiwi routes must satisfy the same seam, or session mode is
// available to one vendor's customers and not the others'.
func TestAllProvidersAreToolRunners(t *testing.T) {
	for name, p := range map[string]any{
		"anthropic": NewAnthropicProviderWithModels("k", "claude-sonnet-5", "claude-sonnet-5"),
		"gemini":    NewGeminiProviderWithModels("k", "gemini-flash-latest", "gemini-flash-latest"),
		"openai":    NewOpenAIProviderWithModels("k", "gpt-5-mini", "gpt-5-mini"),
	} {
		if _, ok := AsToolRunner(p); !ok {
			t.Errorf("%s provider must satisfy ToolRunner", name)
		}
	}
}

func TestOpenAIToolConversationRoundTrip(t *testing.T) {
	var bodies []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		var m map[string]any
		_ = json.Unmarshal(b, &m)
		bodies = append(bodies, m)

		w.Header().Set("Content-Type", "application/json")
		if len(bodies) == 1 {
			_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"looking","tool_calls":[{"id":"call_1","type":"function","function":{"name":"read_file","arguments":"{\"path\":\"main.go\"}"}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":100,"completion_tokens":10,"prompt_tokens_details":{"cached_tokens":80}}}`))
			return
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"all done"},"finish_reason":"stop"}],"usage":{"prompt_tokens":150,"completion_tokens":5}}`))
	}))
	defer srv.Close()

	p := NewOpenAIProviderWithModels("k", "gpt-4.1", "gpt-4.1")
	p.baseURL = srv.URL
	conv := p.StartConversation("you are the implementer", toolDefs(), ConversationOpts{})

	first, err := conv.Send(context.Background(), "go", nil)
	if err != nil {
		t.Fatalf("first send: %v", err)
	}
	if len(first.Calls) != 1 || first.Calls[0].Name != "read_file" || first.Calls[0].ID != "call_1" {
		t.Fatalf("tool call not decoded: %+v", first.Calls)
	}
	var args struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(first.Calls[0].Input, &args); err != nil || args.Path != "main.go" {
		t.Fatalf("arguments = %s (%v)", first.Calls[0].Input, err)
	}

	second, err := conv.Send(context.Background(), "", []ToolResult{{CallID: "call_1", Content: "package main"}})
	if err != nil {
		t.Fatalf("second send: %v", err)
	}
	if !second.Done || second.Text != "all done" {
		t.Errorf("second turn = %+v", second)
	}

	// The tool result must be its own message carrying the call id, or the API
	// rejects the request.
	msgs, _ := bodies[1]["messages"].([]any)
	var sawToolMsg bool
	for _, m := range msgs {
		mm := m.(map[string]any)
		if mm["role"] == "tool" && mm["tool_call_id"] == "call_1" {
			sawToolMsg = true
		}
	}
	if !sawToolMsg {
		t.Errorf("expected a tool message carrying the call id, got %v", msgs)
	}
}

// Cached prompt tokens are reported inside prompt_tokens, so charging both
// would double-count the same tokens at two rates.
func TestOpenAICachedTokensAreNotDoubleCounted(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"hi"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1000,"completion_tokens":0,"prompt_tokens_details":{"cached_tokens":900}}}`))
	}))
	defer srv.Close()

	p := NewOpenAIProviderWithModels("k", "gpt-4.1", "gpt-4.1")
	p.baseURL = srv.URL
	conv := p.StartConversation("s", nil, ConversationOpts{})
	if _, err := conv.Send(context.Background(), "go", nil); err != nil {
		t.Fatal(err)
	}

	u := conv.Usage()
	if u.InputTokens != 100 || u.CacheReadTokens != 900 {
		t.Fatalf("usage = %+v, want 100 uncached and 900 cached", u)
	}
	if want := ModelCostUSDWithCache("gpt-4.1", 100, 0, 900, 0); u.CostUSD != want {
		t.Errorf("cost = %v, want %v", u.CostUSD, want)
	}
}

// A truncated turn's tool arguments may be half-written JSON; appending them
// would corrupt every turn that follows.
func TestOpenAITruncatedTurnIsAnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"half"},"finish_reason":"length"}],"usage":{"prompt_tokens":10,"completion_tokens":10}}`))
	}))
	defer srv.Close()

	p := NewOpenAIProviderWithModels("k", "gpt-4.1", "gpt-4.1")
	p.baseURL = srv.URL
	conv := p.StartConversation("s", nil, ConversationOpts{})
	if _, err := conv.Send(context.Background(), "go", nil); err == nil {
		t.Fatal("a truncated turn must be an error, not an answer")
	}
}

func TestGeminiToolConversationRoundTrip(t *testing.T) {
	var bodies []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		var m map[string]any
		_ = json.Unmarshal(b, &m)
		bodies = append(bodies, m)

		w.Header().Set("Content-Type", "application/json")
		if len(bodies) == 1 {
			_, _ = w.Write([]byte(`{"candidates":[{"content":{"role":"model","parts":[{"functionCall":{"name":"read_file","args":{"path":"main.go"}}}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":100,"candidatesTokenCount":10}}`))
			return
		}
		_, _ = w.Write([]byte(`{"candidates":[{"content":{"role":"model","parts":[{"text":"all done"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":150,"candidatesTokenCount":5}}`))
	}))
	defer srv.Close()

	p := NewGeminiProviderWithModels("k", "gemini-flash-latest", "gemini-flash-latest")
	p.baseURL = srv.URL
	conv := p.StartConversation("you are the implementer", toolDefs(), ConversationOpts{})

	first, err := conv.Send(context.Background(), "go", nil)
	if err != nil {
		t.Fatalf("first send: %v", err)
	}
	if len(first.Calls) != 1 || first.Calls[0].Name != "read_file" {
		t.Fatalf("tool call not decoded: %+v", first.Calls)
	}
	if first.Calls[0].ID == "" {
		t.Error("a call id must be synthesised: Gemini has none and the seam requires one")
	}

	second, err := conv.Send(context.Background(), "", []ToolResult{{CallID: first.Calls[0].ID, Content: "package main"}})
	if err != nil {
		t.Fatalf("second send: %v", err)
	}
	if !second.Done || second.Text != "all done" {
		t.Errorf("second turn = %+v", second)
	}

	// Gemini identifies a call by NAME, so the synthetic id must be translated
	// back or the response is unmatched.
	contents, _ := bodies[1]["contents"].([]any)
	last := contents[len(contents)-1].(map[string]any)
	parts := last["parts"].([]any)
	fr := parts[0].(map[string]any)["functionResponse"].(map[string]any)
	if fr["name"] != "read_file" {
		t.Errorf("function response name = %v, want read_file", fr["name"])
	}
}

// Gemini rejects a parameters schema with no properties, so a tool that takes
// no arguments must send none at all.
func TestGeminiOmitsEmptyParameterSchemas(t *testing.T) {
	p := NewGeminiProviderWithModels("k", "gemini-flash-latest", "gemini-flash-latest")
	conv := p.StartConversation("s", toolDefs(), ConversationOpts{}).(*geminiConversation)

	decls := conv.tools[0].FunctionDeclarations
	if len(decls) != 2 {
		t.Fatalf("expected two declarations, got %d", len(decls))
	}
	if decls[0].Parameters == nil {
		t.Error("a tool with properties must declare them")
	}
	if decls[1].Parameters != nil {
		t.Errorf("a tool with no properties must omit the schema, got %v", decls[1].Parameters)
	}
}

func TestGeminiCachedTokensAreNotDoubleCounted(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"candidates":[{"content":{"role":"model","parts":[{"text":"hi"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":1000,"cachedContentTokenCount":800,"candidatesTokenCount":0}}`))
	}))
	defer srv.Close()

	p := NewGeminiProviderWithModels("k", "gemini-flash-latest", "gemini-flash-latest")
	p.baseURL = srv.URL
	conv := p.StartConversation("s", nil, ConversationOpts{})
	if _, err := conv.Send(context.Background(), "go", nil); err != nil {
		t.Fatal(err)
	}
	u := conv.Usage()
	if u.InputTokens != 200 || u.CacheReadTokens != 800 {
		t.Fatalf("usage = %+v, want 200 uncached and 800 cached", u)
	}
}

func TestGeminiBlockedTurnIsAnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"promptFeedback":{"blockReason":"SAFETY"},"usageMetadata":{}}`))
	}))
	defer srv.Close()

	p := NewGeminiProviderWithModels("k", "gemini-flash-latest", "gemini-flash-latest")
	p.baseURL = srv.URL
	conv := p.StartConversation("s", nil, ConversationOpts{})
	if _, err := conv.Send(context.Background(), "go", nil); err == nil {
		t.Fatal("a blocked turn must be an error")
	}
}
