package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// Reading a provider's response body, and saying what went wrong when it does
// not arrive whole.
//
// Every one of these clients used to write `body, _ := io.ReadAll(resp.Body)`
// and hand the result to json.Unmarshal. That discards the only evidence there
// is: a read that fails mid-body, a connection that drops, and a deadline that
// lands between the headers and the last byte all become an empty or partial
// slice, and encoding/json then reports the one thing that is never the cause —
//
//	decode openai response: unexpected end of JSON input
//
// An Architect review failed with exactly that. It is unactionable: it names
// the decoder, not the timeout or the gateway, and the daemon logs no status
// code, so nothing downstream can tell those cases apart afterwards. The
// helpers below keep the read error and name the deadline, because the whole
// point of an error string is to say which of these happened.

// bodySnippetLimit bounds how much of an undecodable payload is quoted. Enough
// to recognise an HTML error page or a gateway's JSON; short enough to sit in a
// task's failure detail without burying it.
const bodySnippetLimit = 300

// readAPIBody reads a provider response and reports why it could not be read.
//
// name is the provider's own label ("openai", "gemini") so the message says
// which endpoint failed when several are in play.
func readAPIBody(ctx context.Context, name string, resp *http.Response) ([]byte, error) {
	body, err := io.ReadAll(resp.Body)
	if err == nil {
		return body, nil
	}

	// The status line already arrived, so the request was accepted and the
	// failure is in transit. The caller's context is checked first: a deadline
	// or a cancellation is the caller's own budget running out, which calls for
	// a bigger budget rather than a bug report about the provider.
	if cerr := ctx.Err(); cerr != nil {
		return nil, fmt.Errorf("%s response %d was cut off after %d bytes: %w",
			name, resp.StatusCode, len(body), cerr)
	}
	return nil, fmt.Errorf("%s response %d could not be read after %d bytes: %w",
		name, resp.StatusCode, len(body), err)
}

// decodeAPIBody unmarshals a provider response that arrived with a successful
// status, distinguishing "nothing came back" from "what came back is not the
// JSON we expect".
//
// An empty body on a 2xx is its own failure. It happens — a gateway between
// Kiwi and the model can answer 200 with nothing, and a 204 satisfies the
// status check just as well — and it is not a decoding problem, so it does not
// get a decoding error.
func decodeAPIBody(name string, status int, body []byte, v any) error {
	if len(bytes.TrimSpace(body)) == 0 {
		return fmt.Errorf("%s returned %d with an empty body", name, status)
	}
	if err := json.Unmarshal(body, v); err != nil {
		return fmt.Errorf("decode %s response: %w (%d bytes: %s)",
			name, err, len(body), bodySnippet(body))
	}
	return nil
}

// bodySnippet renders the head of a payload for an error message: bounded, on
// one line, so a wall of HTML cannot swamp the log or the task's failure detail.
func bodySnippet(body []byte) string {
	s := strings.Join(strings.Fields(string(body)), " ")
	if len(s) > bodySnippetLimit {
		return s[:bodySnippetLimit] + "…"
	}
	return s
}
