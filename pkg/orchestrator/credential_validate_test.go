package orchestrator

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDefaultCredValidator(t *testing.T) {
	// A stub that returns 200 for token "good" and 401 otherwise, for whichever
	// provider endpoint we point at it.
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ok := r.Header.Get("x-api-key") == "good" ||
			r.Header.Get("Authorization") == "Bearer good" ||
			r.URL.Query().Get("key") == "good"
		if ok {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer stub.Close()

	// Point every provider endpoint at the stub.
	anthropicValidateURL, geminiValidateURL, githubValidateURL = stub.URL, stub.URL, stub.URL

	cases := []struct {
		name    string
		cred    string
		value   string
		wantErr bool
	}{
		{"anthropic good", "ANTHROPIC_API_KEY", "good", false},
		{"anthropic bad", "ANTHROPIC_API_KEY", "bad", true},
		{"gemini good", "GEMINI_API_KEY", "good", false},
		{"gemini bad", "GEMINI_API_KEY", "bad", true},
		{"github good", "GITHUB_TOKEN", "good", false},
		{"github bad", "GIT_TOKEN", "bad", true},
		{"unknown name always allowed", "SLACK_TOKEN", "whatever", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := defaultCredValidator(context.Background(), tc.cred, tc.value)
			if tc.wantErr && err == nil {
				t.Errorf("expected rejection for %s=%q", tc.cred, tc.value)
			}
			if !tc.wantErr && err != nil {
				t.Errorf("expected %s=%q to be accepted, got %v", tc.cred, tc.value, err)
			}
		})
	}
}
