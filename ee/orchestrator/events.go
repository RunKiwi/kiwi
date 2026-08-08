// SPDX-License-Identifier: LicenseRef-Kiwi-BSL-1.1
// Copyright (c) 2026 RunKiwi. Licensed under the Business Source License 1.1.
// See ee/LICENSE. This is Control Plane code and is NOT Apache-2.0.

package orchestrator

import "time"

// TaskEvent is a structured, per-phase telemetry record for the Actor-Critic loop.
type TaskEvent struct {
	ID      uint   `json:"id" gorm:"primaryKey;autoIncrement"`
	TaskID  string `json:"task_id" gorm:"index"`
	OrgID   string `json:"org_id" gorm:"index"`
	Step    int    `json:"step"`    // 0 = initial test; 1..N = iterations
	Phase   string `json:"phase"`   // initial_test | actor | critic | test
	Outcome string `json:"outcome"` // pass | fail | proposed | approved | rejected | error
	Detail  string `json:"detail"`
	// Input is the tool arguments for a `tool` phase, as the model wrote them.
	Input        string    `json:"input"`
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

// headOf returns at most the first n characters of s. The counterpart to
// summarize, for fields whose meaning is at the start: a tool call's path,
// pattern or command is written before its bulk.
func headOf(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// eventTime prefers the daemon's own stamp and falls back to now.
//
// The fallback is what keeps a daemon built before this field from writing rows
// with a zero timestamp — it degrades to the previous behaviour (the flush
// instant) rather than to something that sorts before every real event.
func eventTime(at time.Time) time.Time {
	if at.IsZero() {
		return time.Now()
	}
	return at.UTC()
}
