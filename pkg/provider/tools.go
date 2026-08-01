package provider

import (
	"context"
	"encoding/json"
)

// This file defines Kiwi's tool-calling seam: the ability for a model to ask
// the host to run something and to keep reasoning with the answer.
//
// Nothing in Kiwi had this before. The two existing entry points —
// GetCodeEdit and Complete — are both single-turn: one prompt in, one answer
// out, no way for the model to look at the repository before deciding. That is
// why the Actor is handed a file it did not choose, by a planner that never saw
// the repo. A model that can grep does not need to be told which file to edit.
//
// The seam is deliberately narrow. It describes a conversation, not an agent:
// the caller owns the loop, decides which tools exist, executes them, and
// enforces every limit. Providers implement transport and nothing else.

// ToolDef describes one tool offered to a tool-using model. Properties and
// Required are the body of a JSON Schema object — the schema's "type":"object"
// wrapper is supplied by the provider, since each spells it differently.
type ToolDef struct {
	Name        string
	Description string
	Properties  map[string]any
	Required    []string
}

// ToolCall is one invocation the model asked for. Input is the raw JSON of the
// arguments, left undecoded so the host can unmarshal it into whatever shape
// the named tool expects.
type ToolCall struct {
	ID    string
	Name  string
	Input json.RawMessage
}

// ToolResult is the host's answer to a ToolCall. IsError reports a tool-level
// failure (the command exited non-zero, the path did not exist) — which is
// ordinary feedback the model should see and react to, not a transport error.
// A transport error is returned from Send instead.
type ToolResult struct {
	CallID  string
	Content string
	IsError bool
}

// Turn is one assistant turn. A turn either asks for tools or ends; Done
// reports the latter, which is how the caller learns the model considers itself
// finished rather than waiting for a tool it never requested.
type Turn struct {
	Text  string
	Calls []ToolCall
	Done  bool
}

// ToolUsage is the cumulative accounting for a conversation.
//
// It separates cache reads from cache writes because they are priced
// differently and, in a tool loop, they dominate: every turn re-sends every
// prior turn, so an uncached 40-turn round pays for its transcript 40 times.
// Reporting them as plain input tokens would make a cached run look identical
// to an uncached one on a bill that differs by an order of magnitude.
type ToolUsage struct {
	InputTokens      int64
	OutputTokens     int64
	CacheReadTokens  int64
	CacheWriteTokens int64
	CostUSD          float64
}

// Add accumulates one call's usage into u.
func (u *ToolUsage) Add(v ToolUsage) {
	u.InputTokens += v.InputTokens
	u.OutputTokens += v.OutputTokens
	u.CacheReadTokens += v.CacheReadTokens
	u.CacheWriteTokens += v.CacheWriteTokens
	u.CostUSD += v.CostUSD
}

// ToolConversation is a stateful, multi-turn tool-using exchange with one
// model. It is not safe for concurrent use: turns are inherently ordered, and
// the transcript it accumulates is the conversation.
//
// Its lifetime is deliberately short — one round of work. Kiwi does not persist
// a ToolConversation anywhere, because a provider's message format is not a
// durable representation: three providers are first-class here, a task may be
// retried on a different one, and a half-written transcript with a pending tool
// call is not something worth resuming. Durable state lives in git and in the
// session event log instead.
type ToolConversation interface {
	// Send appends content and returns the model's next turn. On the first call
	// text carries the initial prompt; afterwards results carries the answers to
	// the previous turn's calls. Supplying both is valid — some hosts add a note
	// alongside tool output — and supplying neither is an error.
	Send(ctx context.Context, text string, results []ToolResult) (Turn, error)
	// Usage reports cumulative usage across every turn so far.
	Usage() ToolUsage
	// Transcript reports the number of turns exchanged, so a caller can enforce
	// a cap without tracking it separately.
	Turns() int
}

// TranscriptReporter is implemented by conversations that can report how large
// their transcript has become, so a caller can decide when to compact.
//
// It is separate from ToolConversation because the answer is necessarily an
// estimate — it comes from the last request's billed input, not from a local
// tokenizer — and a provider that cannot give one should be able to say so by
// not implementing this, rather than by returning a number that looks
// authoritative and is not.
type TranscriptReporter interface {
	TranscriptTokens() int64
}

// ToolRunner is a provider that can hold a tool-using conversation. Providers
// that cannot are still perfectly usable as an Actor or an Architect — those
// need only Complete — so this is a separate interface rather than an addition
// to Provider, and a caller checks for it with a type assertion.
type ToolRunner interface {
	StartConversation(system string, tools []ToolDef, opts ConversationOpts) ToolConversation
}

// ConversationOpts tunes one conversation.
type ConversationOpts struct {
	// MaxTokens caps each individual response. Zero uses CompletionBudget().
	MaxTokens int
	// Cache enables prompt caching for the stable prefix (system prompt and tool
	// definitions) and a rolling breakpoint over the transcript. It is off by
	// default so a provider that bills cache writes cannot surprise a caller
	// that did not ask for it.
	Cache bool
	// CompactAt is the transcript token count above which the conversation asks
	// its caller to compact. Zero disables the signal. The conversation never
	// compacts on its own — what is safe to drop is the caller's judgment.
	CompactAt int64
}

// ErrNoToolSupport reports that a provider cannot hold a tool-using
// conversation, so the caller must fall back to the single-turn loop.
type ErrNoToolSupport struct{ Model string }

func (e *ErrNoToolSupport) Error() string {
	return "model " + e.Model + " does not support tool use in this build; use the single-file loop instead"
}

// AsToolRunner returns p as a ToolRunner, reporting whether it is one.
func AsToolRunner(p any) (ToolRunner, bool) {
	tr, ok := p.(ToolRunner)
	return tr, ok
}
