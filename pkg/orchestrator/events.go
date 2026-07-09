package orchestrator

import "time"

// TaskEvent is a structured, per-phase telemetry record for the Actor-Critic loop.
type TaskEvent struct {
	ID           uint      `json:"id" gorm:"primaryKey;autoIncrement"`
	TaskID       string    `json:"task_id" gorm:"index"`
	OrgID        string    `json:"org_id" gorm:"index"`
	Step         int       `json:"step"`    // 0 = initial test; 1..N = iterations
	Phase        string    `json:"phase"`   // initial_test | actor | critic | test
	Outcome      string    `json:"outcome"` // pass | fail | proposed | approved | rejected | error
	Detail       string    `json:"detail"`
	DurationMs   int64     `json:"duration_ms"`
	InputTokens  int64     `json:"input_tokens"`
	OutputTokens int64     `json:"output_tokens"`
	CostUSD      float64   `json:"cost_usd"`
	CreatedAt    time.Time `json:"created_at"`
}

// summarize returns at most the last n characters of s (recent output is the
// most useful for a truncated telemetry detail field).
func summarize(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}
