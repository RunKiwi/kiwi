package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

// --- wire types for tool-using chat/completions ---

type openaiToolDef struct {
	Type     string `json:"type"`
	Function struct {
		Name        string         `json:"name"`
		Description string         `json:"description,omitempty"`
		Parameters  map[string]any `json:"parameters"`
	} `json:"function"`
}

type openaiToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

// openaiToolMessage extends openaiMessage with the fields a tool exchange
// needs. It is separate rather than an addition to openaiMessage so the
// single-turn path keeps emitting exactly the JSON it emits today.
type openaiToolMessage struct {
	Role       string           `json:"role"`
	Content    string           `json:"content,omitempty"`
	ToolCalls  []openaiToolCall `json:"tool_calls,omitempty"`
	ToolCallID string           `json:"tool_call_id,omitempty"`
}

type openaiToolRequest struct {
	Model               string              `json:"model"`
	Messages            []openaiToolMessage `json:"messages"`
	Tools               []openaiToolDef     `json:"tools,omitempty"`
	MaxTokens           int                 `json:"max_tokens,omitempty"`
	MaxCompletionTokens int                 `json:"max_completion_tokens,omitempty"`
}

type openaiToolResponse struct {
	Choices []struct {
		Message struct {
			Content   string           `json:"content"`
			Refusal   string           `json:"refusal"`
			ToolCalls []openaiToolCall `json:"tool_calls"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens        int64 `json:"prompt_tokens"`
		CompletionTokens    int64 `json:"completion_tokens"`
		PromptTokensDetails struct {
			// OpenAI caches prompt prefixes automatically and reports what it
			// served from cache here. There is nothing to opt into, so
			// ConversationOpts.Cache is a no-op for this provider — but the
			// tokens still have to be priced correctly, which is why they are
			// read rather than folded into PromptTokens.
			CachedTokens int64 `json:"cached_tokens"`
		} `json:"prompt_tokens_details"`
	} `json:"usage"`
	Error struct {
		Message string `json:"message"`
		Type    string `json:"type"`
		Code    string `json:"code"`
	} `json:"error"`
}

// StartConversation implements ToolRunner for OpenAI-compatible models.
func (p *OpenAIProvider) StartConversation(system string, tools []ToolDef, opts ConversationOpts) ToolConversation {
	conv := &openaiConversation{provider: p, opts: opts}
	if system != "" {
		conv.messages = append(conv.messages, openaiToolMessage{Role: "system", Content: system})
	}
	for _, d := range tools {
		var t openaiToolDef
		t.Type = "function"
		t.Function.Name = d.Name
		t.Function.Description = d.Description
		props := d.Properties
		if props == nil {
			props = map[string]any{}
		}
		t.Function.Parameters = map[string]any{
			"type":       "object",
			"properties": props,
		}
		if len(d.Required) > 0 {
			t.Function.Parameters["required"] = d.Required
		}
		conv.tools = append(conv.tools, t)
	}
	return conv
}

type openaiConversation struct {
	provider *OpenAIProvider
	opts     ConversationOpts
	tools    []openaiToolDef
	messages []openaiToolMessage
	usage    ToolUsage
	turns    int

	lastInputTokens int64
}

func (c *openaiConversation) Usage() ToolUsage        { return c.usage }
func (c *openaiConversation) Turns() int              { return c.turns }
func (c *openaiConversation) TranscriptTokens() int64 { return c.lastInputTokens }

func (c *openaiConversation) Send(ctx context.Context, text string, results []ToolResult) (Turn, error) {
	if text == "" && len(results) == 0 {
		return Turn{}, fmt.Errorf("openai conversation: Send needs text or tool results")
	}

	// Tool results are their own messages here, unlike Anthropic where they are
	// blocks inside one user turn. Each must carry the id of the call it answers
	// or the API rejects the request.
	for _, r := range results {
		content := r.Content
		if r.IsError && content == "" {
			content = "the tool reported an error with no output"
		}
		c.messages = append(c.messages, openaiToolMessage{
			Role:       "tool",
			ToolCallID: r.CallID,
			Content:    content,
		})
	}
	if text != "" {
		c.messages = append(c.messages, openaiToolMessage{Role: "user", Content: text})
	}

	maxTokens := c.opts.MaxTokens
	if maxTokens <= 0 {
		maxTokens = CompletionBudget()
	}

	reqBody := openaiToolRequest{
		Model:    c.provider.actorModel,
		Messages: c.messages,
		Tools:    c.tools,
	}
	if isReasoningModel(c.provider.actorModel) {
		reqBody.MaxCompletionTokens = maxTokens
	} else {
		reqBody.MaxTokens = maxTokens
	}

	b, err := json.Marshal(reqBody)
	if err != nil {
		return Turn{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.provider.baseURL+"/chat/completions", bytes.NewReader(b))
	if err != nil {
		return Turn{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.provider.apiKey)

	resp, err := c.provider.http.Do(req)
	if err != nil {
		return Turn{}, fmt.Errorf("%s tool turn failed: %w", c.provider.name, err)
	}
	defer resp.Body.Close()

	body, err := readAPIBody(ctx, c.provider.name, resp)
	if err != nil {
		return Turn{}, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var or openaiToolResponse
		_ = json.Unmarshal(body, &or)
		if or.Error.Message != "" {
			return Turn{}, fmt.Errorf("%s API returned %d: %s", c.provider.name, resp.StatusCode, or.Error.Message)
		}
		return Turn{}, fmt.Errorf("%s API returned %d: %s", c.provider.name, resp.StatusCode, string(body))
	}

	var or openaiToolResponse
	if err := decodeAPIBody(c.provider.name, resp.StatusCode, body, &or); err != nil {
		return Turn{}, err
	}
	c.turns++
	c.record(&or)

	if len(or.Choices) == 0 {
		return Turn{}, fmt.Errorf("%s returned no choices", c.provider.name)
	}
	choice := or.Choices[0]
	if choice.Message.Refusal != "" {
		return Turn{}, fmt.Errorf("tool turn refused: %s", choice.Message.Refusal)
	}
	// A truncated turn's tool-call arguments may be half-written JSON. Appending
	// them would corrupt every turn that follows, so this fails like Complete's.
	if choice.FinishReason == "length" {
		return Turn{}, &ErrTruncated{Budget: maxTokens, Model: c.provider.actorModel}
	}

	c.messages = append(c.messages, openaiToolMessage{
		Role:      "assistant",
		Content:   choice.Message.Content,
		ToolCalls: choice.Message.ToolCalls,
	})

	turn := Turn{Text: choice.Message.Content}
	for _, tc := range choice.Message.ToolCalls {
		args := tc.Function.Arguments
		if args == "" {
			// The API sends arguments as a JSON *string*; an empty one is valid
			// for a tool with no parameters, but the host unmarshals it, so it
			// has to be an object rather than "".
			args = "{}"
		}
		turn.Calls = append(turn.Calls, ToolCall{ID: tc.ID, Name: tc.Function.Name, Input: json.RawMessage(args)})
	}
	turn.Done = len(turn.Calls) == 0
	return turn, nil
}

func (c *openaiConversation) record(or *openaiToolResponse) {
	cached := or.Usage.PromptTokensDetails.CachedTokens
	// PromptTokens is the total including cached; charging both would
	// double-count, so the cached part is subtracted out of the full-rate class.
	uncached := or.Usage.PromptTokens - cached
	if uncached < 0 {
		uncached = 0
	}
	call := ToolUsage{
		InputTokens:     uncached,
		OutputTokens:    or.Usage.CompletionTokens,
		CacheReadTokens: cached,
	}
	call.CostUSD = ModelCostUSDWithCache(c.provider.actorModel, call.InputTokens, call.OutputTokens, call.CacheReadTokens, 0)
	c.usage.Add(call)
	c.lastInputTokens = or.Usage.PromptTokens

	c.provider.lastCost = call.CostUSD
	c.provider.lastInput = or.Usage.PromptTokens
	c.provider.lastOutput = or.Usage.CompletionTokens
}
