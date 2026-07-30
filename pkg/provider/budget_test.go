package provider

import (
	"strings"
	"testing"
)

func TestCompletionBudgetDefault(t *testing.T) {
	t.Setenv("KIWI_COMPLETION_MAX_TOKENS", "")
	if got := CompletionBudget(); got != defaultCompletionBudget {
		t.Errorf("CompletionBudget() = %d, want %d", got, defaultCompletionBudget)
	}
}

func TestCompletionBudgetOverride(t *testing.T) {
	t.Setenv("KIWI_COMPLETION_MAX_TOKENS", "65535")
	if got := CompletionBudget(); got != 65535 {
		t.Errorf("CompletionBudget() = %d, want 65535", got)
	}
}

// A nonsense or non-positive override must fall back to the default rather than
// becoming zero, which the APIs reject outright.
func TestCompletionBudgetRejectsBadValues(t *testing.T) {
	for _, v := range []string{"lots", "0", "-1", "8k"} {
		t.Setenv("KIWI_COMPLETION_MAX_TOKENS", v)
		if got := CompletionBudget(); got != defaultCompletionBudget {
			t.Errorf("override %q: got %d, want the %d default", v, got, defaultCompletionBudget)
		}
	}
}

// The truncation error has to say what to do about it — that is the whole reason
// it exists rather than being folded into a generic failure.
func TestErrTruncatedMessageIsActionable(t *testing.T) {
	err := &ErrTruncated{Budget: 16000, Model: "gemini-flash-latest"}
	msg := err.Error()

	for _, want := range []string{"16000", "gemini-flash-latest", "KIWI_COMPLETION_MAX_TOKENS"} {
		if !strings.Contains(msg, want) {
			t.Errorf("message should mention %q, got: %s", want, msg)
		}
	}
}
