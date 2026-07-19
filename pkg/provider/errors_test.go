package provider

import (
	"errors"
	"testing"
)

func TestClassify(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want ErrorKind
	}{
		{"anthropic_credits", errors.New(`400 Bad Request {"message":"Your credit balance is too low to access the Anthropic API."}`), ErrCredits},
		{"insufficient_quota", errors.New("insufficient_quota: you exceeded your current quota"), ErrCredits},
		{"gemini_rate_limit", errors.New("gemini API returned 429: RESOURCE_EXHAUSTED"), ErrRateLimit},
		{"gemini_model_gone", errors.New(`gemini API returned 404: {"message":"This model models/gemini-2.5-flash is no longer available"}`), ErrModelUnavailable},
		{"bad_key", errors.New("401 Unauthorized: invalid x-api-key"), ErrAuth},
		{"gemini_bad_key", errors.New("API key not valid. Please pass a valid API key."), ErrAuth},
		{"unknown", errors.New("connection reset by peer"), ErrOther},
		{"nil", nil, ErrOther},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			kind, reason := Classify(tc.err)
			if kind != tc.want {
				t.Errorf("Classify kind = %d, want %d (reason %q)", kind, tc.want, reason)
			}
			if tc.want != ErrOther && reason == "" {
				t.Error("a classified error should carry a non-empty reason")
			}
		})
	}
}
