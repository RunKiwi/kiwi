package provider

import (
	"context"
	"encoding/json"
	"strings"
)

// Verdict is the Critic's judgment on a proposed edit.
type Verdict struct {
	Approved bool   `json:"approved"`
	Reasons  string `json:"reasons"`
}

// UnmarshalJSON accepts the shapes a model actually emits for `reasons`.
//
// The prompt asks for a string, and models frequently answer with a list
// instead — one bullet per objection, which is a perfectly reasonable reading
// of "reasons" plural. Against a plain string field that is a hard unmarshal
// error:
//
//	json: cannot unmarshal array into Go struct field Verdict.reasons of type string
//
// and parseVerdict fails safe by treating any parse error as a rejection. So a
// Critic that was merely formatting its answer differently read as three
// rejections in a row and failed the task — with the Actor's work possibly
// fine, and the surfaced reason being a Go type error rather than a review.
//
// A list is joined rather than truncated to its first item: each entry is a
// separate objection and the Actor needs all of them to fix the edit in one
// pass. Anything else is kept as its raw JSON, which is more useful to whoever
// reads the task detail than an empty string would be.
func (v *Verdict) UnmarshalJSON(b []byte) error {
	// An alias to avoid recursing into this method.
	type verdictFields struct {
		Approved bool            `json:"approved"`
		Reasons  json.RawMessage `json:"reasons"`
	}
	var raw verdictFields
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	v.Approved = raw.Approved
	v.Reasons = reasonsText(raw.Reasons)
	return nil
}

// reasonsText renders the `reasons` value as one string.
func reasonsText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}

	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}

	var list []string
	if err := json.Unmarshal(raw, &list); err == nil {
		return strings.Join(list, "; ")
	}

	// A list of objects ({"reason": "..."} and similar) is common enough to be
	// worth flattening rather than dumping: pull any string leaf out in order.
	var objs []map[string]any
	if err := json.Unmarshal(raw, &objs); err == nil {
		var parts []string
		for _, o := range objs {
			for _, key := range []string{"reason", "message", "detail", "text"} {
				if s, ok := o[key].(string); ok && s != "" {
					parts = append(parts, s)
					break
				}
			}
		}
		if len(parts) > 0 {
			return strings.Join(parts, "; ")
		}
	}

	// Unrecognised shape — the raw JSON still tells a reader what the Critic
	// said, which beats discarding it.
	return strings.TrimSpace(string(raw))
}

// Critic reviews a proposed edit before it is applied.
type Critic interface {
	ReviewEdit(ctx context.Context, task, fileName, oldContent, newContent, buildOutput string) (Verdict, error)
}

// UsageReporter is implemented by providers that can report the USD cost of
// their most recent API call, so the engine can enforce its budget.
type UsageReporter interface {
	LastCostUSD() float64
}

// TokenReporter is implemented by providers that can report the input/output
// token counts of their most recent API call, for observability.
type TokenReporter interface {
	LastUsage() (inputTokens, outputTokens int64)
}

// MockCritic auto-approves every edit, for offline/test runs.
type MockCritic struct{}

func NewMockCritic() *MockCritic { return &MockCritic{} }

func (m *MockCritic) ReviewEdit(ctx context.Context, task, fileName, oldContent, newContent, buildOutput string) (Verdict, error) {
	return Verdict{Approved: true, Reasons: "mock critic auto-approves"}, nil
}
