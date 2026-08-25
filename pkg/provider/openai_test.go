package provider

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// openaiTestServer returns an httptest server that mimics chat/completions, the
// last request body it received, and a provider pointed at it.
func openaiTestServer(t *testing.T, respBody string, status int) (*httptest.Server, *OpenAIProvider, *openaiRequest) {
	t.Helper()
	var got openaiRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
			t.Errorf("path = %s, want /chat/completions", r.URL.Path)
		}
		if auth := r.Header.Get("Authorization"); auth != "Bearer sk-test-123" {
			t.Errorf("Authorization = %q, want a bearer of the api key", auth)
		}
		// The key must never appear in the URL — it belongs in a header, where it
		// cannot end up in a proxy or access log.
		if strings.Contains(r.URL.String(), "sk-test-123") {
			t.Errorf("api key leaked into URL: %s", r.URL.String())
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.WriteHeader(status)
		_, _ = w.Write([]byte(respBody))
	}))
	op := NewOpenAIProviderWithModels("sk-test-123", "gpt-4.1-mini", "gpt-4.1-mini")
	op.baseURL = srv.URL
	op.http = srv.Client()
	return srv, op, &got
}

func chatResponse(content, finish string, in, out int64) string {
	b, _ := json.Marshal(map[string]any{
		"choices": []any{map[string]any{
			"message":       map[string]any{"content": content},
			"finish_reason": finish,
		}},
		"usage": map[string]any{"prompt_tokens": in, "completion_tokens": out},
	})
	return string(b)
}

func TestOpenAIGetCodeEdit(t *testing.T) {
	body := chatResponse("Here is the fix:\n```go\npackage x // FIXED\n```", "stop", 100, 50)
	srv, op, _ := openaiTestServer(t, body, http.StatusOK)
	defer srv.Close()

	code, err := op.GetCodeEdit(context.Background(), "fix it", "x.go", "package x // broken", "boom")
	if err != nil {
		t.Fatalf("GetCodeEdit: %v", err)
	}
	if code != "package x // FIXED" {
		t.Errorf("code = %q, want the fenced content", code)
	}
	if in, out := op.LastUsage(); in != 100 || out != 50 {
		t.Errorf("usage = %d/%d, want 100/50", in, out)
	}
	if op.LastCostUSD() <= 0 {
		t.Errorf("expected a positive recorded cost, got %v", op.LastCostUSD())
	}
}

func TestOpenAIReviewEdit(t *testing.T) {
	body := chatResponse(`{"approved": true, "reasons": "looks correct"}`, "stop", 20, 10)
	srv, op, _ := openaiTestServer(t, body, http.StatusOK)
	defer srv.Close()

	v, err := op.ReviewEdit(context.Background(), "fix it", "x.go", "old", "new", "out")
	if err != nil {
		t.Fatalf("ReviewEdit: %v", err)
	}
	if !v.Approved {
		t.Errorf("verdict should be approved, got %+v", v)
	}
}

func TestOpenAIAPIErrorSurfacedWithoutKey(t *testing.T) {
	srv, op, _ := openaiTestServer(t,
		`{"error":{"message":"You exceeded your current quota","type":"insufficient_quota"}}`,
		http.StatusTooManyRequests)
	defer srv.Close()

	_, err := op.GetCodeEdit(context.Background(), "t", "x.go", "code", "out")
	if err == nil {
		t.Fatal("expected an error on non-2xx response")
	}
	if strings.Contains(err.Error(), "sk-test-123") {
		t.Errorf("error leaked the api key: %v", err)
	}
	if !strings.Contains(err.Error(), "exceeded your current quota") {
		t.Errorf("error should surface the API message, got %v", err)
	}
}

// Classify turns a raw provider error into the message shown on a job. A
// billing failure that lands in ErrOther reaches the user as a raw JSON dump,
// which is what this pairing exists to prevent.
func TestOpenAIQuotaErrorIsClassifiedAsCredits(t *testing.T) {
	srv, op, _ := openaiTestServer(t,
		`{"error":{"message":"You exceeded your current quota, please check your plan and billing details.","type":"insufficient_quota"}}`,
		http.StatusTooManyRequests)
	defer srv.Close()

	_, err := op.Complete(context.Background(), "s", "u")
	if err == nil {
		t.Fatal("expected an error")
	}
	kind, reason := Classify(err)
	if kind != ErrCredits {
		t.Errorf("kind = %v, want ErrCredits so the job says to add credits; reason = %q", kind, reason)
	}
}

// A refusal arrives as a populated `refusal` field with empty content. Treated
// as an ordinary empty answer it would look like the model returned nothing.
func TestOpenAIRefusalIsReported(t *testing.T) {
	body := `{"choices":[{"message":{"content":"","refusal":"I can't help with that"},"finish_reason":"stop"}],"usage":{}}`
	srv, op, _ := openaiTestServer(t, body, http.StatusOK)
	defer srv.Close()

	_, err := op.GetCodeEdit(context.Background(), "t", "x.go", "code", "out")
	if err == nil || !strings.Contains(err.Error(), "refused") {
		t.Errorf("expected a refusal error, got %v", err)
	}
}

// Some reasoning-family models behind an OpenAI-compatible endpoint (e.g.
// OpenRouter) return their whole answer in the "reasoning" field and leave
// "content" empty, with finish_reason "stop" — not "length", so the
// truncation guard never fires. Without a fallback that reads as an empty
// answer, which a caller like session.parseSpec cannot tell apart from a
// model that just answered in prose.
func TestOpenAIFallsBackToReasoningWhenContentIsEmpty(t *testing.T) {
	body := `{"choices":[{"message":{"content":"","reasoning":"{\"verdict\":\"proceed\"}"},"finish_reason":"stop"}],"usage":{}}`
	srv, op, _ := openaiTestServer(t, body, http.StatusOK)
	defer srv.Close()

	text, err := op.Complete(context.Background(), "sys", "user")
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if text != `{"verdict":"proceed"}` {
		t.Errorf("text = %q, want the reasoning field's content", text)
	}
}

// The same guarantee the other two providers make: a response cut off at the
// output limit is an error, never partial text passed off as a whole answer.
func TestOpenAICompleteReportsTruncation(t *testing.T) {
	body := chatResponse(`{"files":[{"path":"a.js","content":"half a fi`, "length", 10, 16000)
	srv, op, _ := openaiTestServer(t, body, http.StatusOK)
	defer srv.Close()

	text, err := op.Complete(context.Background(), "system", "user")
	if err == nil {
		t.Fatalf("expected a truncation error, got text %q", text)
	}
	var trunc *ErrTruncated
	if !errors.As(err, &trunc) {
		t.Fatalf("error should be *ErrTruncated so callers can act on it, got %T: %v", err, err)
	}
	if text != "" {
		t.Errorf("a truncated response must not also return partial text, got %q", text)
	}
}

// A 200 that carries an error object instead of choices is real on
// OpenAI-compatible gateways. Reading it as an empty answer would surface as
// "no choices", hiding what the endpoint actually said.
func TestOpenAIErrorInsideA200IsSurfaced(t *testing.T) {
	srv, op, _ := openaiTestServer(t, `{"error":{"message":"model overloaded"}}`, http.StatusOK)
	defer srv.Close()

	_, err := op.Complete(context.Background(), "s", "u")
	if err == nil || !strings.Contains(err.Error(), "model overloaded") {
		t.Errorf("expected the embedded error surfaced, got %v", err)
	}
}

// The reasoning families reject max_tokens and any non-default temperature with
// a 400. Sending the wrong pair means every task on a gpt-5 model fails before
// the loop starts, so the request shape is asserted rather than assumed.
func TestOpenAIReasoningModelsUseCompletionTokenCeiling(t *testing.T) {
	srv, op, got := openaiTestServer(t, chatResponse("ok", "stop", 1, 1), http.StatusOK)
	defer srv.Close()
	op.actorModel = "gpt-5-mini"

	if _, err := op.Complete(context.Background(), "s", "u"); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if got.MaxCompletionTokens == 0 {
		t.Error("a reasoning model must be sent max_completion_tokens")
	}
	if got.MaxTokens != 0 {
		t.Errorf("max_tokens must not be sent to a reasoning model, got %d", got.MaxTokens)
	}
	if got.Temperature != nil {
		t.Errorf("temperature must be omitted for a reasoning model, got %v", *got.Temperature)
	}
}

// The complement: the older chat models take max_tokens, and a low temperature
// is worth keeping there because the Actor's job is a precise edit.
func TestOpenAIChatModelsUseMaxTokens(t *testing.T) {
	srv, op, got := openaiTestServer(t, chatResponse("ok", "stop", 1, 1), http.StatusOK)
	defer srv.Close()

	if _, err := op.Complete(context.Background(), "s", "u"); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if got.MaxTokens == 0 {
		t.Error("a chat model must be sent max_tokens")
	}
	if got.MaxCompletionTokens != 0 {
		t.Errorf("max_completion_tokens must not be sent to a chat model, got %d", got.MaxCompletionTokens)
	}
	if got.Temperature == nil || *got.Temperature != 0.2 {
		t.Errorf("temperature = %v, want 0.2", got.Temperature)
	}
}

func TestIsReasoningModel(t *testing.T) {
	for _, m := range []string{"gpt-5", "gpt-5-mini", "GPT-5-nano", "o1-preview", "o3-mini", "o4-mini"} {
		if !isReasoningModel(m) {
			t.Errorf("%q should be treated as a reasoning model", m)
		}
	}
	for _, m := range []string{"gpt-4o", "gpt-4.1-mini", "gpt-3.5-turbo"} {
		if isReasoningModel(m) {
			t.Errorf("%q is a chat model and must keep max_tokens", m)
		}
	}
}

// Pricing guards that an OpenAI model is never billed at Anthropic's much
// higher fallback rate, which is what happened to any unlisted model before
// ModelCostUSD learned about a third family.
func TestOpenAIPricingUsed(t *testing.T) {
	cost := ModelCostUSD("gpt-4o-mini", 1_000_000, 1_000_000)
	want := 0.15 + 0.60
	if cost != want {
		t.Errorf("gpt-4o-mini cost = %v, want %v", cost, want)
	}
	// An unlisted OpenAI model falls back within its own family, not to Opus.
	unlisted := ModelCostUSD("gpt-6-turbo", 1_000_000, 0)
	if unlisted != 0.25 {
		t.Errorf("unlisted OpenAI model fell back to wrong pricing: %v", unlisted)
	}
	if unlisted == ModelCostUSD("claude-opus-4-8", 1_000_000, 0) {
		t.Error("an OpenAI model must not be priced at Anthropic rates")
	}
}

func TestOpenAIEmbed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/embeddings") {
			t.Errorf("path = %s, want /embeddings", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"data":[{"embedding":[0.1,0.2,0.3]}]}`))
	}))
	defer srv.Close()

	op := NewOpenAIProviderWithModels("sk-test-123", "", "")
	op.baseURL = srv.URL
	op.http = srv.Client()

	vec, err := op.Embed(context.Background(), "hello")
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(vec) != 3 {
		t.Errorf("embedding length = %d, want 3", len(vec))
	}
}

// An OpenAI-compatible endpoint (Azure, a gateway, a local server) is
// configured by env, not code. If the override is ignored every request goes to
// api.openai.com with a key that endpoint never issued.
func TestOpenAIBaseURLOverride(t *testing.T) {
	t.Setenv("KIWI_OPENAI_BASE_URL", "https://gateway.example.com/v1/")
	op := NewOpenAIProviderWithModels("sk-x", "", "")
	if op.baseURL != "https://gateway.example.com/v1" {
		t.Errorf("baseURL = %q, want the override with its trailing slash trimmed", op.baseURL)
	}
}
