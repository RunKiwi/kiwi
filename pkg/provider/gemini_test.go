package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// geminiTestServer returns an httptest server that mimics generateContent and a
// provider pointed at it.
func geminiTestServer(t *testing.T, respBody string, status int) (*httptest.Server, *GeminiProvider) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if !strings.Contains(r.URL.Path, ":generateContent") {
			t.Errorf("path = %s, want :generateContent", r.URL.Path)
		}
		if got := r.Header.Get("x-goog-api-key"); got != "gk-123" {
			t.Errorf("api key header = %q, want gk-123", got)
		}
		// The key must never appear in the URL (it is a header).
		if strings.Contains(r.URL.String(), "gk-123") {
			t.Errorf("api key leaked into URL: %s", r.URL.String())
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(respBody))
	}))
	gp := NewGeminiProviderWithModels("gk-123", "gemini-2.0-flash", "gemini-2.0-flash")
	gp.baseURL = srv.URL
	gp.http = srv.Client()
	return srv, gp
}

func TestGeminiGetCodeEdit(t *testing.T) {
	// The text value keeps \n as JSON escape sequences (raw strings), so the
	// fixture is valid JSON; backticks are spliced in via a normal string.
	resp := `{"candidates":[{"content":{"parts":[{"text":"Here is the fix:\n` +
		"```" + `go\npackage x // FIXED\n` + "```" +
		`"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":100,"candidatesTokenCount":50}}`
	srv, gp := geminiTestServer(t, resp, http.StatusOK)
	defer srv.Close()

	code, err := gp.GetCodeEdit(context.Background(), "fix it", "x.go", "package x // broken", "boom")
	if err != nil {
		t.Fatalf("GetCodeEdit: %v", err)
	}
	if code != "package x // FIXED" {
		t.Errorf("code = %q, want the fenced content", code)
	}
	if in, out := gp.LastUsage(); in != 100 || out != 50 {
		t.Errorf("usage = %d/%d, want 100/50", in, out)
	}
	if gp.LastCostUSD() <= 0 {
		t.Errorf("expected a positive recorded cost, got %v", gp.LastCostUSD())
	}
}

func TestGeminiReviewEdit(t *testing.T) {
	resp := `{
		"candidates":[{"content":{"parts":[{"text":"{\"approved\": true, \"reasons\": \"looks correct\"}"}]},"finishReason":"STOP"}],
		"usageMetadata":{"promptTokenCount":20,"candidatesTokenCount":10}
	}`
	srv, gp := geminiTestServer(t, resp, http.StatusOK)
	defer srv.Close()

	v, err := gp.ReviewEdit(context.Background(), "fix it", "x.go", "old", "new", "out")
	if err != nil {
		t.Fatalf("ReviewEdit: %v", err)
	}
	if !v.Approved {
		t.Errorf("verdict should be approved, got %+v", v)
	}
}

func TestGeminiAPIErrorSurfacedWithoutKey(t *testing.T) {
	srv, gp := geminiTestServer(t, `{"error":{"code":429,"message":"quota exceeded"}}`, http.StatusTooManyRequests)
	defer srv.Close()

	_, err := gp.GetCodeEdit(context.Background(), "t", "x.go", "code", "out")
	if err == nil {
		t.Fatal("expected an error on non-2xx response")
	}
	if strings.Contains(err.Error(), "gk-123") {
		t.Errorf("error leaked the api key: %v", err)
	}
	if !strings.Contains(err.Error(), "quota exceeded") {
		t.Errorf("error should surface the API message, got %v", err)
	}
}

func TestGeminiSafetyBlock(t *testing.T) {
	resp := `{"candidates":[{"content":{"parts":[{"text":""}]},"finishReason":"SAFETY"}],"usageMetadata":{}}`
	srv, gp := geminiTestServer(t, resp, http.StatusOK)
	defer srv.Close()

	_, err := gp.GetCodeEdit(context.Background(), "t", "x.go", "code", "out")
	if err == nil || !strings.Contains(err.Error(), "safety") {
		t.Errorf("expected a safety-block error, got %v", err)
	}
}

// TestGeminiPricingUsed guards that gemini models are priced from the gemini
// table, not the (much higher) Anthropic fallback.
func TestGeminiPricingUsed(t *testing.T) {
	cost := ModelCostUSD("gemini-2.0-flash", 1_000_000, 1_000_000)
	want := 0.10 + 0.40
	if cost != want {
		t.Errorf("gemini cost = %v, want %v (should use gemini pricing)", cost, want)
	}
	// An unlisted gemini model falls back to gemini pricing, not opus.
	unlisted := ModelCostUSD("gemini-3.0-ultra", 1_000_000, 0)
	if unlisted != 0.10 {
		t.Errorf("unlisted gemini fell back to wrong pricing: %v", unlisted)
	}
}
