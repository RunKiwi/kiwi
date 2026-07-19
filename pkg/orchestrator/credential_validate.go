package orchestrator

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Provider endpoints hit to confirm a credential is accepted. Overridable in
// tests so validation never makes a real external call there.
var (
	anthropicValidateURL = "https://api.anthropic.com/v1/models"
	geminiValidateURL    = "https://generativelanguage.googleapis.com/v1beta/models"
	githubValidateURL    = "https://api.github.com/user"
)

// credValidateClient bounds how long a save waits on the provider.
var credValidateClient = &http.Client{Timeout: 8 * time.Second}

// defaultCredValidator makes a lightweight, read-only call to confirm a provider
// credential is accepted before it is saved, so a typo'd or revoked key is
// caught at save time instead of surfacing as a mysterious task failure later.
//
// It fails CLOSED on a definitive rejection (the provider says the key is bad)
// and OPEN on everything else — an unknown credential name, or a network blip —
// so a transient outage never blocks saving a good key. It checks that the key
// is valid, not that it has credits/quota; that is surfaced per-task instead.
func defaultCredValidator(ctx context.Context, name, value string) error {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()

	switch name {
	case "ANTHROPIC_API_KEY":
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, anthropicValidateURL, nil)
		req.Header.Set("x-api-key", value)
		req.Header.Set("anthropic-version", "2023-06-01")
		return checkCredential(req, "Anthropic")
	case "GEMINI_API_KEY":
		u := geminiValidateURL + "?key=" + url.QueryEscape(value)
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		return checkCredential(req, "Gemini")
	case "GITHUB_TOKEN", "GIT_TOKEN":
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, githubValidateURL, nil)
		req.Header.Set("Authorization", "Bearer "+value)
		req.Header.Set("Accept", "application/vnd.github+json")
		return checkCredential(req, "GitHub")
	default:
		// SLACK_TOKEN and anything else: we do not have a cheap check, so allow it.
		return nil
	}
}

// checkCredential performs the request and returns a user-facing error only when
// the provider definitively rejects the credential (auth failure, or a body that
// says the key is invalid). Network errors return nil (fail open).
func checkCredential(req *http.Request, provider string) error {
	resp, err := credValidateClient.Do(req)
	if err != nil {
		return nil // transient — do not block a save on a network blip
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return fmt.Errorf("%s rejected this credential (HTTP %d) — check the key and try again", provider, resp.StatusCode)
	}
	// Gemini returns 400 API_KEY_INVALID (not 401) for a bad key; catch that by
	// body inspection without over-rejecting other 400s.
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		low := strings.ToLower(string(body))
		if strings.Contains(low, "api_key_invalid") ||
			strings.Contains(low, "api key not valid") ||
			strings.Contains(low, "invalid api key") {
			return fmt.Errorf("%s rejected this credential (invalid key) — check the key and try again", provider)
		}
	}
	return nil
}
