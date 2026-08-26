package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// The failure that started this: an Architect review came back as
//
//	architect review failed: openai completion request failed:
//	decode openai response: unexpected end of JSON input
//
// "unexpected end of JSON input" is what encoding/json says about an empty
// slice, so the message describes the decoder's disappointment and says nothing
// about why there was nothing to decode. Each test below is one of the ways
// that body fails to arrive, and each must name itself.

func TestOpenAIChat_EmptyBodyIsNotReportedAsADecodeFailure(t *testing.T) {
	srv, op, _ := openaiTestServer(t, "", http.StatusOK)
	defer srv.Close()

	_, _, err := op.chat(context.Background(), "gpt-4.1-mini", "sys", "user", 100, false)
	if err == nil {
		t.Fatal("expected an error for a 200 with no body")
	}
	if strings.Contains(err.Error(), "unexpected end of JSON input") {
		t.Errorf("still reporting the decoder's symptom: %v", err)
	}
	if !strings.Contains(err.Error(), "empty body") {
		t.Errorf("error should name the empty body, got: %v", err)
	}
}

// A body that stops early is a transport failure, not a decode failure. The
// read error carries that and used to be discarded on the spot: `body, _ :=
// io.ReadAll(resp.Body)`.
func TestOpenAIChat_TruncatedBodyReportsTheReadFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Promise more than is delivered, then return: the server closes the
		// connection and the client's read ends early.
		w.Header().Set("Content-Length", "4096")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"con`))
	}))
	defer srv.Close()

	op := NewOpenAIProviderWithModels("sk-test-123", "gpt-4.1-mini", "gpt-4.1-mini")
	op.baseURL = srv.URL
	op.http = srv.Client()

	_, _, err := op.chat(context.Background(), "gpt-4.1-mini", "sys", "user", 100, false)
	if err == nil {
		t.Fatal("expected an error for a truncated body")
	}
	if strings.Contains(err.Error(), "unexpected end of JSON input") {
		t.Errorf("still reporting the decoder's symptom: %v", err)
	}
	if !strings.Contains(err.Error(), "could not be read") {
		t.Errorf("error should name the failed read, got: %v", err)
	}
}

// The Architect reviews at the end of a round, on the session context — so the
// task's own budget is the deadline most likely to land mid-response. That the
// call ran out of time is the single most useful thing to say, and it is
// exactly what the old message hid.
func TestOpenAIChat_DeadlineDuringBodyNamesTheDeadline(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "4096")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"choices":[{"message":{"con`))
		w.(http.Flusher).Flush()
		<-r.Context().Done() // hold the body open until the client gives up
	}))
	defer srv.Close()

	op := NewOpenAIProviderWithModels("sk-test-123", "gpt-4.1-mini", "gpt-4.1-mini")
	op.baseURL = srv.URL
	op.http = srv.Client()

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	_, _, err := op.chat(ctx, "gpt-4.1-mini", "sys", "user", 100, false)
	if err == nil {
		t.Fatal("expected an error when the deadline lands mid-body")
	}
	if strings.Contains(err.Error(), "unexpected end of JSON input") {
		t.Errorf("still reporting the decoder's symptom: %v", err)
	}
	if !strings.Contains(err.Error(), "deadline") {
		t.Errorf("error should name the deadline, got: %v", err)
	}
}

// The user who hit this had not configured OpenAI at all, and the error still
// said "openai": OpenRouter is openai_compatible, so it is served by this
// client, and every message here used to be hardcoded. Naming a provider the
// user never chose sends them to the wrong integration to fix a key that is not
// the one in use.
func TestOpenAICompatible_ErrorsNameTheProviderActuallyInUse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK) // 200, no body
	}))
	defer srv.Close()

	op := NewOpenAICompatibleProvider("or-key", "moonshotai/kimi-k2", "moonshotai/kimi-k2", srv.URL, ProviderOpenRouter)
	op.http = srv.Client()

	for _, tc := range []struct {
		name string
		call func() error
	}{
		{"Complete", func() error { _, err := op.Complete(context.Background(), "sys", "user"); return err }},
		{"GetCodeEdit", func() error {
			_, err := op.GetCodeEdit(context.Background(), "t", "f.go", "code", "out")
			return err
		}},
		{"ReviewEdit", func() error {
			_, err := op.ReviewEdit(context.Background(), "t", "f.go", "a", "b", "out")
			return err
		}},
	} {
		err := tc.call()
		if err == nil {
			t.Fatalf("%s: expected an error", tc.name)
		}
		if !strings.Contains(err.Error(), "openrouter") {
			t.Errorf("%s: error should name openrouter, got: %v", tc.name, err)
		}
		if strings.Contains(err.Error(), "openai") {
			t.Errorf("%s: error names openai, which the user never configured: %v", tc.name, err)
		}
	}
}

// Plain OpenAI keeps saying openai — the label follows the provider, it is not
// simply erased.
func TestOpenAIProvider_StillNamesItself(t *testing.T) {
	srv, op, _ := openaiTestServer(t, "", http.StatusOK)
	defer srv.Close()

	_, err := op.Complete(context.Background(), "sys", "user")
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "openai") {
		t.Errorf("error should name openai, got: %v", err)
	}
}

// Malformed JSON that did arrive is a genuine decode failure and keeps saying
// so — with enough of the payload to see what the endpoint actually sent.
func TestOpenAIChat_MalformedJSONStillDecodesAsSuch(t *testing.T) {
	srv, op, _ := openaiTestServer(t, "<html>502 Bad Gateway</html>", http.StatusOK)
	defer srv.Close()

	_, _, err := op.chat(context.Background(), "gpt-4.1-mini", "sys", "user", 100, false)
	if err == nil {
		t.Fatal("expected an error for a non-JSON body")
	}
	if !strings.Contains(err.Error(), "decode openai response") {
		t.Errorf("error should still name the decode, got: %v", err)
	}
	if !strings.Contains(err.Error(), "502 Bad Gateway") {
		t.Errorf("error should quote what arrived, got: %v", err)
	}
}

// A body long enough to be a wall of text in a task log is cut down, so the
// error stays readable wherever it is surfaced.
func TestBodySnippet_IsBounded(t *testing.T) {
	got := bodySnippet([]byte(strings.Repeat("x", 5000)))
	if len(got) > bodySnippetLimit+len("…") {
		t.Errorf("snippet is %d bytes, want at most %d", len(got), bodySnippetLimit+len("…"))
	}
	if !strings.HasSuffix(got, "…") {
		t.Errorf("a truncated snippet should say so, got %q", got[max(0, len(got)-20):])
	}
}

// Gemini reads its responses the same way and had the same discarded error.
func TestGeminiChat_EmptyBodyIsNotReportedAsADecodeFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	gp := NewGeminiProviderWithModels("gm-test", "gemini-flash-latest", "gemini-flash-latest")
	gp.baseURL = srv.URL
	gp.http = srv.Client()

	_, err := gp.Complete(context.Background(), "sys", "user")
	if err == nil {
		t.Fatal("expected an error for a 200 with no body")
	}
	if strings.Contains(err.Error(), "unexpected end of JSON input") {
		t.Errorf("still reporting the decoder's symptom: %v", err)
	}
	if !strings.Contains(err.Error(), "empty body") {
		t.Errorf("error should name the empty body, got: %v", err)
	}
}
