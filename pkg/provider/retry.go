package provider

import (
	"context"
	"errors"
	"io"
	"math/rand"
	"net/http"
	"strconv"
	"time"
)

// Transient provider failures are retried at the transport layer.
//
// The agentic session loop is what makes this necessary. A session makes dozens
// of model calls per round, so the chance of meeting at least one throttle or
// one 503 approaches certainty as the loop gets longer. Before this, any single blip ended the whole session —
// a task that had already spent minutes and dollars was thrown away because one
// call in fifty came back 429 with the provider itself saying "Please retry in
// 2.785470319s".
//
// Doing it here rather than in pkg/session means the Architect, the planner and
// the embedder get it too, and the session loop keeps no retry logic of its own.
//
// Retrying a POST is safe for these endpoints specifically: a 429 or a 5xx means
// the request was throttled or failed before producing a completion, so there is
// no half-applied effect to duplicate. This transport must not be reused for a
// request that mutates state.
//
// Only Gemini and OpenAI need it. The Anthropic provider goes through the
// official SDK, which already retries 429 and 5xx (MaxRetries defaults to 2 in
// requestconfig), so wrapping its client too would compound two backoffs.
//
// A retried-away failure also costs nothing: usage is recorded from the decoded
// response, and the swallowed attempts never reach that code, so a throttle does
// not draw down the session budget.
const (
	defaultRetryAttempts = 4
	defaultRetryBase     = 700 * time.Millisecond
	defaultRetryCap      = 12 * time.Second
)

// retryableStatus reports whether a response should be retried.
//
// 429 is throttling. 5xx is the provider failing on its own side; 529 is
// Anthropic's overloaded status and is not in the standard range. A 4xx other
// than 429 is the caller's fault and will fail identically forever, so retrying
// it only burns the deadline.
func retryableStatus(code int) bool {
	switch code {
	case http.StatusTooManyRequests, // 429
		http.StatusInternalServerError, // 500
		http.StatusBadGateway,          // 502
		http.StatusServiceUnavailable,  // 503
		http.StatusGatewayTimeout,      // 504
		529:                            // Anthropic "overloaded"
		return true
	}
	return false
}

type retryTransport struct {
	base     http.RoundTripper
	attempts int
	// sleep is injectable so tests do not spend real seconds.
	sleep func(ctx context.Context, d time.Duration) error
}

func newRetryTransport(base http.RoundTripper) *retryTransport {
	if base == nil {
		base = http.DefaultTransport
	}
	return &retryTransport{base: base, attempts: defaultRetryAttempts, sleep: sleepCtx}
}

// retryingClient is an *http.Client that retries transient provider failures.
// It deliberately does not touch http.DefaultClient, which is process-global.
func retryingClient() *http.Client {
	return &http.Client{Transport: newRetryTransport(nil)}
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func (rt *retryTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	ctx := req.Context()

	// A body that cannot be rewound cannot be retried. http.NewRequest sets
	// GetBody for the in-memory readers every provider here uses; anything else
	// falls through to a single attempt rather than silently sending a
	// half-consumed body.
	replayable := req.Body == nil || req.GetBody != nil

	var lastResp *http.Response
	var lastErr error

	for attempt := 0; ; attempt++ {
		if attempt > 0 {
			body, err := rewind(req)
			if err != nil {
				return lastResp, lastErr
			}
			req = req.Clone(ctx)
			req.Body = body
		}

		resp, err := rt.base.RoundTrip(req)
		lastResp, lastErr = resp, err

		final := attempt >= rt.attempts-1 || !replayable
		switch {
		case err != nil:
			// A transport error (connection reset, DNS blip) is worth one more
			// try; a context cancellation is not — the caller has given up.
			if final || ctx.Err() != nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return nil, err
			}
		case !retryableStatus(resp.StatusCode):
			return resp, nil
		case final:
			// Out of attempts: hand back the real response so the caller reports
			// the provider's own message rather than a synthetic one.
			return resp, nil
		}

		delay := rt.delayFor(attempt, resp)

		// Draining and closing lets the connection be reused; leaving it open
		// leaks one per retry.
		if resp != nil {
			_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
			resp.Body.Close()
		}

		// Never sleep past the caller's deadline. A round has a budget, and
		// burning it inside a backoff produces a timeout instead of the
		// provider error that explains what actually happened.
		if dl, ok := ctx.Deadline(); ok && time.Now().Add(delay).After(dl) {
			if resp != nil {
				return nil, errRetryDeadline
			}
			return nil, lastErr
		}
		if err := rt.sleep(ctx, delay); err != nil {
			return nil, err
		}
	}
}

var errRetryDeadline = errors.New("provider kept failing transiently and the deadline arrived before another retry would fit")

func rewind(req *http.Request) (io.ReadCloser, error) {
	if req.Body == nil || req.GetBody == nil {
		return nil, nil
	}
	return req.GetBody()
}

// delayFor prefers the provider's own instruction. Both Gemini and OpenAI send
// Retry-After on a throttle, and it reflects the real quota window — guessing
// with pure exponential backoff either wastes time or hammers a closed window.
func (rt *retryTransport) delayFor(attempt int, resp *http.Response) time.Duration {
	if resp != nil {
		if d, ok := retryAfter(resp.Header); ok {
			if d > defaultRetryCap {
				return defaultRetryCap
			}
			return d
		}
	}
	// Exponential with full jitter: several tool calls in one round can be
	// throttled together, and a fixed schedule would retry them in lockstep.
	backoff := defaultRetryBase << attempt
	if backoff > defaultRetryCap {
		backoff = defaultRetryCap
	}
	return time.Duration(rand.Int63n(int64(backoff)) + int64(backoff)/2)
}

func retryAfter(h http.Header) (time.Duration, bool) {
	v := h.Get("Retry-After")
	if v == "" {
		return 0, false
	}
	if secs, err := strconv.Atoi(v); err == nil && secs >= 0 {
		return time.Duration(secs) * time.Second, true
	}
	if t, err := http.ParseTime(v); err == nil {
		if d := time.Until(t); d > 0 {
			return d, true
		}
		return 0, true
	}
	return 0, false
}
