package provider

import (
	"context"
	"encoding/json"
	"fmt"
)

// MockToolRunner is a scripted ToolRunner for offline tests.
//
// It exists for the same reason MockProvider does: the session loop's control
// flow — round caps, budget enforcement, stall detection, tool dispatch — is
// the part worth testing, and none of it should need a network. The script
// decides what the "model" asks for on each turn, so a test can drive the loop
// down any path it wants to assert on.
type MockToolRunner struct {
	// Script returns the turn to reply with. n counts from 1. Returning a Turn
	// with no Calls ends the conversation, exactly as a real model does.
	Script func(n int, text string, results []ToolResult) (Turn, error)
	// CostPerTurn is charged to the conversation's usage on every turn, so
	// budget rails are exercised without a provider that reports real cost.
	CostPerTurn float64
	// TokensPerTurn grows the reported transcript size each turn, so a caller's
	// compaction threshold can be reached deterministically.
	TokensPerTurn int64
	// Started records every conversation this runner handed out, so a test can
	// assert on the system prompt and tool set it was given.
	Started []MockConversationStart
}

// MockConversationStart records the arguments of one StartConversation call.
type MockConversationStart struct {
	System string
	Tools  []ToolDef
	Opts   ConversationOpts
}

// StartConversation implements ToolRunner.
func (m *MockToolRunner) StartConversation(system string, tools []ToolDef, opts ConversationOpts) ToolConversation {
	m.Started = append(m.Started, MockConversationStart{System: system, Tools: tools, Opts: opts})
	return &mockConversation{runner: m}
}

type mockConversation struct {
	runner *MockToolRunner
	turns  int
	usage  ToolUsage
	// tokens grows with each turn so a test can drive a caller's compaction
	// threshold without a real model.
	tokens int64
}

// TranscriptTokens implements TranscriptReporter.
func (c *mockConversation) TranscriptTokens() int64 { return c.tokens }

func (c *mockConversation) Turns() int       { return c.turns }
func (c *mockConversation) Usage() ToolUsage { return c.usage }

func (c *mockConversation) Send(ctx context.Context, text string, results []ToolResult) (Turn, error) {
	if err := ctx.Err(); err != nil {
		return Turn{}, err
	}
	c.turns++
	c.tokens += c.runner.TokensPerTurn
	c.usage.Add(ToolUsage{InputTokens: 100, OutputTokens: 50, CostUSD: c.runner.CostPerTurn})
	if c.runner.Script == nil {
		return Turn{Text: "done", Done: true}, nil
	}
	t, err := c.runner.Script(c.turns, text, results)
	if err != nil {
		return Turn{}, err
	}
	t.Done = len(t.Calls) == 0
	return t, nil
}

// MockCall builds a ToolCall with JSON-encoded input, for scripting tests.
func MockCall(id, name string, input any) ToolCall {
	b, err := json.Marshal(input)
	if err != nil {
		panic(fmt.Sprintf("provider.MockCall: %v", err))
	}
	return ToolCall{ID: id, Name: name, Input: b}
}
