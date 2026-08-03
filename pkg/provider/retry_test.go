package provider

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// noSleep swaps the backoff out so the tests assert on behaviour, not duration.
func testTransport(base http.RoundTripper, attempts int) *retryTransport {
	rt := newRetryTransport(base)
	rt.attempts = attempts
	rt.sleep = func(context.Context, time.Duration) error { return nil }
	return rt
}

// The failure that motivated this: one 429 in the middle of a session ended the
// whole task, even though the provider said to retry in under three seconds.
func TestRetriesThrottleThenSucceeds(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&calls, 1) == 1 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":"rate limited"}`))
			return
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	c := &http.Client{Transport: testTransport(nil, 4)}
	resp, err := c.Post(srv.URL, "application/json", strings.NewReader(`{"q":1}`))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Errorf("upstream calls = %d, want 2 (one throttle, one success)", got)
	}
}

// 503 was the second transient code seen in production, hours after the 429.
func TestRetriesServerErrors(t *testing.T) {
	for _, code := range []int{500, 502, 503, 504, 529} {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			var calls int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if atomic.AddInt32(&calls, 1) == 1 {
					w.WriteHeader(code)
					return
				}
				_, _ = w.Write([]byte(`{"ok":true}`))
			}))
			defer srv.Close()

			c := &http.Client{Transport: testTransport(nil, 3)}
			resp, err := c.Post(srv.URL, "application/json", strings.NewReader(`{}`))
			if err != nil {
				t.Fatalf("post: %v", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				t.Errorf("status = %d after retrying %d, want 200", resp.StatusCode, code)
			}
		})
	}
}

// A 4xx that is not 429 is the caller's fault and fails identically forever.
// Retrying it only burns the round's deadline.
func TestDoesNotRetryClientErrors(t *testing.T) {
	for _, code := range []int{400, 401, 403, 404, 422} {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			var calls int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				atomic.AddInt32(&calls, 1)
				w.WriteHeader(code)
			}))
			defer srv.Close()

			c := &http.Client{Transport: testTransport(nil, 4)}
			resp, err := c.Post(srv.URL, "application/json", strings.NewReader(`{}`))
			if err != nil {
				t.Fatalf("post: %v", err)
			}
			defer resp.Body.Close()
			if got := atomic.LoadInt32(&calls); got != 1 {
				t.Errorf("%d was retried %d times; it must not be retried", code, got-1)
			}
		})
	}
}

// The request body must be replayed intact. A retry that sent an empty or
// half-consumed body would turn a throttle into a malformed-request error.
func TestReplaysTheRequestBody(t *testing.T) {
	var bodies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		bodies = append(bodies, string(b))
		if len(bodies) == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	const payload = `{"messages":[{"role":"user","content":"hello"}]}`
	c := &http.Client{Transport: testTransport(nil, 3)}
	resp, err := c.Post(srv.URL, "application/json", strings.NewReader(payload))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()

	if len(bodies) != 2 {
		t.Fatalf("expected 2 upstream calls, got %d", len(bodies))
	}
	if bodies[0] != payload || bodies[1] != payload {
		t.Errorf("body not replayed intact:\n first=%q\nsecond=%q", bodies[0], bodies[1])
	}
}

// When the retries run out the caller must see the provider's own response, not
// a synthetic error — the real message is what explains the failure in the task
// log ("quota exceeded ... limit: 5").
func TestReturnsTheProviderResponseWhenAttemptsRunOut(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"message":"quota exceeded, limit: 5"}}`))
	}))
	defer srv.Close()

	c := &http.Client{Transport: testTransport(nil, 3)}
	resp, err := c.Post(srv.URL, "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusTooManyRequests {
		t.Errorf("status = %d, want the real 429", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "quota exceeded") {
		t.Errorf("provider message lost: %q", body)
	}
	if got := atomic.LoadInt32(&calls); got != 3 {
		t.Errorf("attempts = %d, want the configured 3", got)
	}
}

// Retry must not outlive the caller's deadline. A round has a time budget, and
// sleeping past it converts a useful provider error into a bare timeout.
func TestStopsWhenTheContextIsDone(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	rt := newRetryTransport(nil)
	rt.attempts = 10
	rt.sleep = sleepCtx // real sleeping, so the deadline actually binds

	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, srv.URL, strings.NewReader(`{}`))
	c := &http.Client{Transport: rt}
	if _, err := c.Do(req); err == nil {
		t.Fatal("expected an error once the deadline passed")
	}
	if got := atomic.LoadInt32(&calls); got > 3 {
		t.Errorf("kept retrying past the deadline: %d calls", got)
	}
}

func TestRetryAfterParsing(t *testing.T) {
	tests := []struct {
		name, header string
		want         time.Duration
		ok           bool
	}{
		{"absent", "", 0, false},
		{"seconds", "3", 3 * time.Second, true},
		{"zero", "0", 0, true},
		{"garbage", "soon", 0, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := http.Header{}
			if tc.header != "" {
				h.Set("Retry-After", tc.header)
			}
			got, ok := retryAfter(h)
			if ok != tc.ok || got != tc.want {
				t.Errorf("retryAfter(%q) = (%v, %v), want (%v, %v)", tc.header, got, ok, tc.want, tc.ok)
			}
		})
	}
}

// A 503 used to classify as ErrOther, so the task detail said nothing useful.
func TestClassifyRecognisesTransientServerFailures(t *testing.T) {
	for _, msg := range []string{
		"gemini API returned 503: service unavailable",
		"anthropic: overloaded_error",
		"openai API returned 500: internal error",
	} {
		if kind, reason := Classify(errFromString(msg)); kind != ErrTransient {
			t.Errorf("Classify(%q) = %v (%q), want ErrTransient", msg, kind, reason)
		}
	}
}

// A model-not-found must not be swallowed by the transient case above it.
func TestClassifyStillDetectsAnUnavailableModel(t *testing.T) {
	if kind, _ := Classify(errFromString("gemini: model gemini-2.5-flash is no longer available")); kind != ErrModelUnavailable {
		t.Errorf("model-not-found misclassified as %v", kind)
	}
}

type stringErr string

func (e stringErr) Error() string { return string(e) }

func errFromString(s string) error { return stringErr(s) }
