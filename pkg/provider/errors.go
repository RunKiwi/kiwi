package provider

import "strings"

// ErrorKind categorises a provider (LLM) API failure so callers can turn a raw
// API error into a clear, actionable message instead of dumping the response.
type ErrorKind int

const (
	// ErrOther is any failure we do not specifically recognise.
	ErrOther ErrorKind = iota
	// ErrAuth: the API key was rejected (missing, invalid, or unauthorized).
	ErrAuth
	// ErrCredits: the account has no remaining credits / billing is exhausted.
	ErrCredits
	// ErrRateLimit: the request was throttled or a rate/quota window is exhausted.
	ErrRateLimit
	// ErrModelUnavailable: the requested model does not exist or is not available
	// to this key.
	ErrModelUnavailable
)

// Classify inspects a provider error (Anthropic or Gemini) and returns a kind
// plus a human-readable reason with no provider name and no raw payload — the
// caller prefixes the provider. It matches on the substrings these APIs return
// so it works regardless of which SDK surfaced the error.
func Classify(err error) (ErrorKind, string) {
	if err == nil {
		return ErrOther, ""
	}
	s := strings.ToLower(err.Error())

	switch {
	// Billing exhausted. Anthropic: "credit balance is too low". Codex-style /
	// compatible: "insufficient_quota", "billing".
	case strings.Contains(s, "credit balance") ||
		strings.Contains(s, "insufficient_quota") ||
		strings.Contains(s, "insufficient credit") ||
		strings.Contains(s, "payment required") ||
		strings.Contains(s, " 402"):
		return ErrCredits, "the account is out of credits or its billing quota is exhausted — add credits or switch to a configured provider"

	// Throttling / quota window. Gemini: RESOURCE_EXHAUSTED / 429.
	case strings.Contains(s, "rate limit") ||
		strings.Contains(s, "rate_limit") ||
		strings.Contains(s, "resource_exhausted") ||
		strings.Contains(s, "too many requests") ||
		strings.Contains(s, " 429"):
		return ErrRateLimit, "the provider is rate-limiting or the quota window is exhausted — retry shortly or raise the account's limits"

	// Bad model. Gemini: "is no longer available", NOT_FOUND / 404 on a model.
	case strings.Contains(s, "no longer available") ||
		strings.Contains(s, "model not found") ||
		strings.Contains(s, "is not found") ||
		(strings.Contains(s, "model") && strings.Contains(s, "not_found")) ||
		(strings.Contains(s, "model") && strings.Contains(s, " 404")):
		return ErrModelUnavailable, "the selected model is unavailable for this key — pick a different model"

	// Auth. 401/403, invalid key, API key not valid, permission denied.
	case strings.Contains(s, "invalid x-api-key") ||
		strings.Contains(s, "api key not valid") ||
		strings.Contains(s, "invalid api key") ||
		strings.Contains(s, "api_key_invalid") ||
		strings.Contains(s, "authentication") ||
		strings.Contains(s, "unauthorized") ||
		strings.Contains(s, "permission denied") ||
		strings.Contains(s, " 401") ||
		strings.Contains(s, " 403"):
		return ErrAuth, "the API key was rejected (invalid or unauthorized) — re-check the key under Integrations"

	default:
		return ErrOther, ""
	}
}
