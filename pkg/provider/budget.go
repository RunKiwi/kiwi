package provider

import (
	"fmt"
	"os"
	"strconv"
)

// defaultCompletionBudget is the output-token ceiling for Complete, the
// general-purpose single-turn call. It is the binding constraint on the
// multi-file Actor, which must return whole file contents as JSON, so it is
// deliberately generous compared with the Critic's.
//
// It is not unbounded because output tokens are the expensive half of a request
// and this runs on the customer's key: a runaway response should hit a ceiling
// rather than a bill.
const defaultCompletionBudget = 16000

// CompletionBudget is the output-token ceiling for Complete, overridable with
// KIWI_COMPLETION_MAX_TOKENS.
//
// It is configurable because the right value is a property of the *model*, not
// of Kiwi: model output ceilings differ by an order of magnitude (8k on older
// Flash generations, 64k+ on newer ones), and a value above what the model
// accepts is rejected by the API. Operators running a smaller model can lower
// it; those running a larger one can raise it without a code change.
func CompletionBudget() int {
	if v := os.Getenv("KIWI_COMPLETION_MAX_TOKENS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return defaultCompletionBudget
}

// ErrTruncated reports that the model stopped because it hit the output-token
// ceiling, so its answer is incomplete.
//
// This is its own error type because truncation is *actionable* in a way a
// generic failure is not — the fix is a bigger budget or a smaller request — and
// because the alternative is what shipped before: the partial text was returned
// as if it were a whole answer, and the first thing to notice was whatever
// parser choked on it downstream. "unexpected end of JSON input" describes the
// symptom; this names the cause.
type ErrTruncated struct {
	Budget int
	Model  string
}

func (e *ErrTruncated) Error() string {
	return fmt.Sprintf(
		"the model's response was cut off at the %d-token output limit for %s; "+
			"the task likely needs fewer or smaller files, or a higher KIWI_COMPLETION_MAX_TOKENS",
		e.Budget, e.Model,
	)
}
