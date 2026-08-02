package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// --- wire types for tool-using generateContent ---

type geminiFunctionDecl struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty"`
}

type geminiTool struct {
	FunctionDeclarations []geminiFunctionDecl `json:"functionDeclarations"`
}

type geminiFunctionCall struct {
	Name string          `json:"name"`
	Args json.RawMessage `json:"args,omitempty"`
}

type geminiFunctionResponse struct {
	Name     string         `json:"name"`
	Response map[string]any `json:"response"`
}

// geminiToolPart is a part that may carry text or a function call/response.
// Separate from geminiPart so the single-turn path keeps sending exactly the
// JSON it sends today.
type geminiToolPart struct {
	Text             string                  `json:"text,omitempty"`
	FunctionCall     *geminiFunctionCall     `json:"functionCall,omitempty"`
	FunctionResponse *geminiFunctionResponse `json:"functionResponse,omitempty"`
}

type geminiToolContent struct {
	Role  string           `json:"role,omitempty"`
	Parts []geminiToolPart `json:"parts"`
}

type geminiToolRequest struct {
	SystemInstruction *geminiToolContent  `json:"systemInstruction,omitempty"`
	Contents          []geminiToolContent `json:"contents"`
	Tools             []geminiTool        `json:"tools,omitempty"`
	GenerationConfig  struct {
		MaxOutputTokens int `json:"maxOutputTokens,omitempty"`
	} `json:"generationConfig"`
}

type geminiToolResponse struct {
	Candidates []struct {
		Content      geminiToolContent `json:"content"`
		FinishReason string            `json:"finishReason"`
	} `json:"candidates"`
	UsageMetadata struct {
		PromptTokenCount int64 `json:"promptTokenCount"`
		// CachedContentTokenCount is what Gemini served from its implicit
		// context cache. Like OpenAI there is nothing to opt into, so
		// ConversationOpts.Cache is a no-op here — but the tokens are priced
		// differently and must not be billed at the full input rate.
		CachedContentTokenCount int64 `json:"cachedContentTokenCount"`
		CandidatesTokenCount    int64 `json:"candidatesTokenCount"`
	} `json:"usageMetadata"`
	PromptFeedback struct {
		BlockReason string `json:"blockReason"`
	} `json:"promptFeedback"`
}

// StartConversation implements ToolRunner for Gemini models.
func (p *GeminiProvider) StartConversation(system string, tools []ToolDef, opts ConversationOpts) ToolConversation {
	conv := &geminiConversation{provider: p, opts: opts}
	if system != "" {
		conv.system = &geminiToolContent{Parts: []geminiToolPart{{Text: system}}}
	}
	if len(tools) > 0 {
		decls := make([]geminiFunctionDecl, 0, len(tools))
		for _, d := range tools {
			decl := geminiFunctionDecl{Name: d.Name, Description: d.Description}
			// Gemini rejects a parameters schema with no properties, so a tool
			// that takes no arguments sends none at all rather than an empty
			// object.
			if len(d.Properties) > 0 {
				params := map[string]any{"type": "object", "properties": d.Properties}
				if len(d.Required) > 0 {
					params["required"] = d.Required
				}
				decl.Parameters = params
			}
			decls = append(decls, decl)
		}
		conv.tools = []geminiTool{{FunctionDeclarations: decls}}
	}
	return conv
}

type geminiConversation struct {
	provider *GeminiProvider
	opts     ConversationOpts
	system   *geminiToolContent
	tools    []geminiTool
	contents []geminiToolContent
	usage    ToolUsage
	turns    int

	lastInputTokens int64
	// callNames maps the synthetic call ids handed to the host back to the
	// function names Gemini expects in a response. Gemini identifies a call by
	// name rather than by id, so the id Kiwi's seam requires is invented here
	// and translated back on the way in.
	callNames map[string]string
}

func (c *geminiConversation) Usage() ToolUsage        { return c.usage }
func (c *geminiConversation) Turns() int              { return c.turns }
func (c *geminiConversation) TranscriptTokens() int64 { return c.lastInputTokens }

func (c *geminiConversation) Send(ctx context.Context, text string, results []ToolResult) (Turn, error) {
	if text == "" && len(results) == 0 {
		return Turn{}, fmt.Errorf("gemini conversation: Send needs text or tool results")
	}

	parts := make([]geminiToolPart, 0, len(results)+1)
	for _, r := range results {
		name := c.callNames[r.CallID]
		if name == "" {
			// Should not happen; answering with the id keeps the exchange
			// well-formed rather than dropping the result on the floor.
			name = r.CallID
		}
		payload := map[string]any{"output": r.Content}
		if r.IsError {
			payload = map[string]any{"error": r.Content}
		}
		parts = append(parts, geminiToolPart{
			FunctionResponse: &geminiFunctionResponse{Name: name, Response: payload},
		})
	}
	if text != "" {
		parts = append(parts, geminiToolPart{Text: text})
	}
	c.contents = append(c.contents, geminiToolContent{Role: "user", Parts: parts})

	maxTokens := c.opts.MaxTokens
	if maxTokens <= 0 {
		maxTokens = CompletionBudget()
	}

	reqBody := geminiToolRequest{
		SystemInstruction: c.system,
		Contents:          c.contents,
		Tools:             c.tools,
	}
	reqBody.GenerationConfig.MaxOutputTokens = maxTokens

	b, err := json.Marshal(reqBody)
	if err != nil {
		return Turn{}, err
	}
	url := fmt.Sprintf("%s/models/%s:generateContent", c.provider.baseURL, c.provider.actorModel)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(b))
	if err != nil {
		return Turn{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	// Header, not query param, so the key cannot leak into URL logs.
	req.Header.Set("x-goog-api-key", c.provider.apiKey)

	resp, err := c.provider.http.Do(req)
	if err != nil {
		return Turn{}, fmt.Errorf("gemini tool turn failed: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Turn{}, fmt.Errorf("gemini API returned %d: %s", resp.StatusCode, string(body))
	}

	var gr geminiToolResponse
	if err := json.Unmarshal(body, &gr); err != nil {
		return Turn{}, fmt.Errorf("decode gemini response: %w", err)
	}
	c.turns++
	c.record(&gr)

	if gr.PromptFeedback.BlockReason != "" {
		return Turn{}, fmt.Errorf("tool turn blocked: %s", gr.PromptFeedback.BlockReason)
	}
	if len(gr.Candidates) == 0 {
		return Turn{}, fmt.Errorf("gemini returned no candidates")
	}
	cand := gr.Candidates[0]
	if cand.FinishReason == "MAX_TOKENS" {
		return Turn{}, &ErrTruncated{Budget: maxTokens, Model: c.provider.actorModel}
	}

	// Echo the model turn so the next request carries the exchange.
	c.contents = append(c.contents, geminiToolContent{Role: "model", Parts: cand.Content.Parts})

	if c.callNames == nil {
		c.callNames = map[string]string{}
	}
	var turn Turn
	for i, part := range cand.Content.Parts {
		if part.Text != "" {
			turn.Text += part.Text
		}
		if part.FunctionCall == nil {
			continue
		}
		// Gemini has no call ids, so one is synthesised per turn and position.
		id := fmt.Sprintf("gem-%d-%d", c.turns, i)
		c.callNames[id] = part.FunctionCall.Name
		args := part.FunctionCall.Args
		if len(args) == 0 {
			args = json.RawMessage("{}")
		}
		turn.Calls = append(turn.Calls, ToolCall{ID: id, Name: part.FunctionCall.Name, Input: args})
	}
	turn.Done = len(turn.Calls) == 0
	return turn, nil
}

func (c *geminiConversation) record(gr *geminiToolResponse) {
	cached := gr.UsageMetadata.CachedContentTokenCount
	uncached := gr.UsageMetadata.PromptTokenCount - cached
	if uncached < 0 {
		uncached = 0
	}
	call := ToolUsage{
		InputTokens:     uncached,
		OutputTokens:    gr.UsageMetadata.CandidatesTokenCount,
		CacheReadTokens: cached,
	}
	call.CostUSD = ModelCostUSDWithCache(c.provider.actorModel, call.InputTokens, call.OutputTokens, call.CacheReadTokens, 0)
	c.usage.Add(call)
	c.lastInputTokens = gr.UsageMetadata.PromptTokenCount

	c.provider.lastCost = call.CostUSD
	c.provider.lastInput = gr.UsageMetadata.PromptTokenCount
	c.provider.lastOutput = gr.UsageMetadata.CandidatesTokenCount
}
