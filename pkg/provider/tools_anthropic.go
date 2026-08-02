package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/anthropics/anthropic-sdk-go"
)

// StartConversation implements ToolRunner for Anthropic models.
func (p *AnthropicProvider) StartConversation(system string, tools []ToolDef, opts ConversationOpts) ToolConversation {
	return &anthropicConversation{
		provider: p,
		system:   system,
		tools:    anthropicTools(tools),
		opts:     opts,
	}
}

// anthropicTools converts Kiwi's provider-neutral tool definitions into the
// SDK's union type. The "type":"object" wrapper is supplied here rather than by
// the caller — every provider spells it differently, and ToolDef exists so the
// session package never has to know which one it is talking to.
func anthropicTools(defs []ToolDef) []anthropic.ToolUnionParam {
	out := make([]anthropic.ToolUnionParam, 0, len(defs))
	for _, d := range defs {
		t := &anthropic.ToolParam{
			Name: d.Name,
			InputSchema: anthropic.ToolInputSchemaParam{
				Properties: d.Properties,
				Required:   d.Required,
			},
		}
		if d.Description != "" {
			t.Description = anthropic.String(d.Description)
		}
		out = append(out, anthropic.ToolUnionParam{OfTool: t})
	}
	return out
}

// anthropicConversation is one tool-using exchange. It owns the message history
// for its lifetime and is not safe for concurrent use.
type anthropicConversation struct {
	provider *AnthropicProvider
	system   string
	tools    []anthropic.ToolUnionParam
	opts     ConversationOpts

	messages []anthropic.MessageParam
	usage    ToolUsage
	turns    int
	// lastInputTokens is the most recent request's total input size, which is
	// the only estimate of transcript size available without a separate
	// tokenizer. CompactAt is compared against it.
	lastInputTokens int64
}

func (c *anthropicConversation) Usage() ToolUsage { return c.usage }
func (c *anthropicConversation) Turns() int       { return c.turns }

// TranscriptTokens reports the size of the last request, which is what a caller
// compares against ConversationOpts.CompactAt.
func (c *anthropicConversation) TranscriptTokens() int64 { return c.lastInputTokens }

// Send appends content and returns the model's next turn.
func (c *anthropicConversation) Send(ctx context.Context, text string, results []ToolResult) (Turn, error) {
	if text == "" && len(results) == 0 {
		return Turn{}, errors.New("anthropic conversation: Send needs text or tool results")
	}

	// Tool results must come first in the user turn: the API pairs them
	// positionally with the tool_use blocks of the preceding assistant turn, and
	// a stray text block ahead of them is rejected.
	blocks := make([]anthropic.ContentBlockParamUnion, 0, len(results)+1)
	for _, r := range results {
		blocks = append(blocks, anthropic.NewToolResultBlock(r.CallID, r.Content, r.IsError))
	}
	if text != "" {
		blocks = append(blocks, anthropic.NewTextBlock(text))
	}
	c.messages = append(c.messages, anthropic.MessageParam{
		Role:    anthropic.MessageParamRoleUser,
		Content: blocks,
	})

	maxTokens := c.opts.MaxTokens
	if maxTokens <= 0 {
		maxTokens = CompletionBudget()
	}

	sys := []anthropic.TextBlockParam{{Text: c.system}}
	if c.opts.Cache {
		c.applyCacheBreakpoints(sys)
	}

	params := anthropic.MessageNewParams{
		Model:     anthropic.Model(c.provider.actorModel),
		MaxTokens: int64(maxTokens),
		System:    sys,
		Messages:  c.messages,
	}
	if len(c.tools) > 0 {
		params.Tools = c.tools
	}

	resp, err := c.provider.client.Messages.New(ctx, params)
	if err != nil {
		return Turn{}, fmt.Errorf("anthropic tool turn failed: %w", err)
	}

	c.turns++
	c.record(resp)

	if resp.StopReason == anthropic.StopReasonRefusal {
		return Turn{}, errors.New("tool turn refused by safety classifier")
	}
	// A truncated turn is not a usable answer: its tool_use blocks may be
	// half-written, and appending them would corrupt the transcript for every
	// turn that follows. Fail loudly, exactly as Complete does.
	if resp.StopReason == anthropic.StopReasonMaxTokens {
		return Turn{}, &ErrTruncated{Budget: maxTokens, Model: c.provider.actorModel}
	}

	// Echo the assistant turn back into the history. The SDK exposes no
	// Message.ToParam(), so the blocks are rebuilt — and only the two kinds that
	// matter here are carried. Thinking blocks are deliberately dropped: they
	// may not be replayed without their signatures, and this loop's continuity
	// comes from tool results, not from prior reasoning.
	var turn Turn
	echo := make([]anthropic.ContentBlockParamUnion, 0, len(resp.Content))
	for _, block := range resp.Content {
		switch v := block.AsAny().(type) {
		case anthropic.TextBlock:
			turn.Text += v.Text
			echo = append(echo, anthropic.NewTextBlock(v.Text))
		case anthropic.ToolUseBlock:
			var input any
			if err := json.Unmarshal(v.Input, &input); err != nil {
				// The API produced the JSON, so this should not happen; if it
				// does, echoing an unparseable block would poison every later
				// turn. Report it instead.
				return Turn{}, fmt.Errorf("anthropic tool turn: decode tool input for %q: %w", v.Name, err)
			}
			turn.Calls = append(turn.Calls, ToolCall{ID: v.ID, Name: v.Name, Input: v.Input})
			echo = append(echo, anthropic.NewToolUseBlock(v.ID, input, v.Name))
		}
	}
	if len(echo) > 0 {
		c.messages = append(c.messages, anthropic.NewAssistantMessage(echo...))
	}
	turn.Done = len(turn.Calls) == 0
	return turn, nil
}

// applyCacheBreakpoints marks the prefix that is worth caching.
//
// Two breakpoints, matching how the cost actually falls. The system prompt and
// tool definitions are byte-identical for the whole round and often for the
// whole task, so they get the long TTL. The transcript grows monotonically, so
// a breakpoint on the newest user turn makes every earlier turn a cache read —
// which is the difference between paying for the transcript once and paying for
// it on every turn.
//
// The rolling breakpoint is placed on the *previous* user turn rather than the
// one just appended: a breakpoint caches everything before it, so marking the
// newest turn would write a cache entry that the next request immediately
// invalidates by appending to it.
func (c *anthropicConversation) applyCacheBreakpoints(sys []anthropic.TextBlockParam) {
	if len(sys) > 0 {
		sys[len(sys)-1].CacheControl = anthropic.CacheControlEphemeralParam{TTL: anthropic.CacheControlEphemeralTTLTTL1h}
	}

	// Clear any breakpoint set on a previous request so at most one rolling
	// breakpoint exists; the API caps how many a request may carry.
	for _, m := range c.messages {
		for _, b := range m.Content {
			if cc := b.GetCacheControl(); cc != nil {
				*cc = anthropic.CacheControlEphemeralParam{}
			}
		}
	}

	// Find the last user message before the one just appended.
	for i := len(c.messages) - 2; i >= 0; i-- {
		if c.messages[i].Role != anthropic.MessageParamRoleUser {
			continue
		}
		blocks := c.messages[i].Content
		if len(blocks) == 0 {
			return
		}
		if cc := blocks[len(blocks)-1].GetCacheControl(); cc != nil {
			*cc = anthropic.CacheControlEphemeralParam{TTL: anthropic.CacheControlEphemeralTTLTTL5m}
		}
		return
	}
}

// record accumulates usage and prices the call with its cache token classes.
func (c *anthropicConversation) record(resp *anthropic.Message) {
	u := resp.Usage
	call := ToolUsage{
		InputTokens:      u.InputTokens,
		OutputTokens:     u.OutputTokens,
		CacheReadTokens:  u.CacheReadInputTokens,
		CacheWriteTokens: u.CacheCreationInputTokens,
	}
	call.CostUSD = ModelCostUSDWithCache(c.provider.actorModel, call.InputTokens, call.OutputTokens,
		call.CacheReadTokens, call.CacheWriteTokens)
	c.usage.Add(call)
	c.lastInputTokens = u.InputTokens + u.CacheReadInputTokens + u.CacheCreationInputTokens

	// Keep the single-call reporters consistent, so a conversation driven
	// through this provider still answers LastCostUSD/LastUsage the way the
	// existing loop telemetry expects.
	c.provider.lastCost = call.CostUSD
	c.provider.lastInput = call.InputTokens
	c.provider.lastOutput = call.OutputTokens
}
