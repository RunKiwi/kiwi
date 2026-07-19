package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

// Compile-time proof AnthropicProvider satisfies all three interfaces.
var (
	_ Provider      = (*AnthropicProvider)(nil)
	_ Critic        = (*AnthropicProvider)(nil)
	_ UsageReporter = (*AnthropicProvider)(nil)
)

func TestNewAnthropicProviderConstructs(t *testing.T) {
	p := NewAnthropicProvider("test-key-not-used")
	if p == nil {
		t.Fatal("expected non-nil provider")
	}
	if p.LastCostUSD() != 0 {
		t.Fatalf("expected zero initial cost, got %v", p.LastCostUSD())
	}
}

func anthropicTestServer(t *testing.T, respBody string, status int) (*httptest.Server, *AnthropicProvider) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if !strings.Contains(r.URL.Path, "/v1/messages") {
			t.Errorf("path = %s, want /v1/messages", r.URL.Path)
		}
		if got := r.Header.Get("x-api-key"); got != "ak-123" {
			t.Errorf("api key header = %q, want ak-123", got)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(respBody))
	}))

	client := anthropic.NewClient(
		option.WithAPIKey("ak-123"),
		option.WithBaseURL(srv.URL+"/v1/"),
	)

	ap := &AnthropicProvider{
		client:      client,
		actorModel:  "claude-opus-4-8",
		criticModel: "claude-opus-4-8",
	}
	return srv, ap
}

func TestAnthropicComplete(t *testing.T) {
	resp := `{"id": "msg_123", "type": "message", "role": "assistant", "content": [{"type": "text", "text": "hello anthropic"}], "model": "claude-opus-4-8", "stop_reason": "end_turn", "stop_sequence": null, "usage": {"input_tokens": 10, "output_tokens": 5}}`
	srv, ap := anthropicTestServer(t, resp, http.StatusOK)
	defer srv.Close()

	text, err := ap.Complete(context.Background(), "system", "user")
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if text != "hello anthropic" {
		t.Errorf("Complete = %q, want 'hello anthropic'", text)
	}
	if in, out := ap.LastUsage(); in != 10 || out != 5 {
		t.Errorf("usage = %d/%d, want 10/5", in, out)
	}
}
