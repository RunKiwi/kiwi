package telemetry

import (
	"context"
	"time"
)

var timeZero time.Time

// StubProvider returns a fixed Result regardless of query/range, matching
// the existing stub pattern for Anthropic/Gemini/OpenAI (pkg/provider) so
// CGO_ENABLED=0 go test ./pkg/... never needs real Datadog/Prometheus
// credentials or network access.
type StubProvider struct {
	Result Result
	Err    error
}

func (s *StubProvider) Query(ctx context.Context, query string, start, end time.Time) (Result, error) {
	if s.Err != nil {
		return Result{}, s.Err
	}
	return s.Result, nil
}
