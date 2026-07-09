package provider

import "context"

// Verdict is the Critic's judgment on a proposed edit.
type Verdict struct {
	Approved bool   `json:"approved"`
	Reasons  string `json:"reasons"`
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
