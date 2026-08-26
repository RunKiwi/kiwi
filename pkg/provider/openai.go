package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
)

// defaultOpenAIBaseURL is the Chat Completions API root. Overridable per
// provider so tests never make a real call, and via KIWI_OPENAI_BASE_URL so an
// operator can point Kiwi at an OpenAI-compatible endpoint (Azure, a gateway, a
// self-hosted server) without a code change.
const defaultOpenAIBaseURL = "https://api.openai.com/v1"

// OpenAIProvider is a live OpenAI-backed Actor and Critic. Like GeminiProvider
// it is a thin JSON client over the HTTP API rather than an SDK dependency,
// which keeps the daemon — the process that actually runs the loop — free of a
// third provider SDK and its transitive tree.
type OpenAIProvider struct {
	// name is the provider this client is speaking for, and it appears in every
	// error this file produces. It is a field rather than a constant because
	// OpenAI is not the only provider served here: OpenRouter — the provider
	// Kiwi funds for the free tier — is openai_compatible and runs on this same
	// client. An Architect failure once reported "openai completion request
	// failed" to a user who had never configured OpenAI, which points at an
	// integration they cannot fix because it is not the one in use.
	name        string
	apiKey      string
	actorModel  string
	criticModel string
	baseURL     string
	http        *http.Client

	lastCost   float64
	lastInput  int64
	lastOutput int64
}

// NewOpenAIProviderWithModels builds a provider with customized Actor and Critic
// models (e.g. "gpt-5-mini"). An empty model defaults to gpt-5-mini.
func NewOpenAIProviderWithModels(apiKey, actorModel, criticModel string) *OpenAIProvider {
	if actorModel == "" {
		actorModel = "gpt-5-mini"
	}
	if criticModel == "" {
		criticModel = "gpt-5-mini"
	}
	base := defaultOpenAIBaseURL
	if v := strings.TrimRight(os.Getenv("KIWI_OPENAI_BASE_URL"), "/"); v != "" {
		base = v
	}
	return &OpenAIProvider{
		name:        ProviderOpenAI,
		apiKey:      apiKey,
		actorModel:  actorModel,
		criticModel: criticModel,
		baseURL:     base,
		http:        retryingClient(),
	}
}

// NewOpenAICompatibleProvider builds a provider against an OpenAI-compatible
// endpoint that is not OpenAI itself — OpenRouter today, any registry row with
// Kind == KindOpenAICompatible in general.
//
// The base URL is a parameter rather than an environment lookup because
// KIWI_OPENAI_BASE_URL is deployment-wide: setting it to reach OpenRouter would
// simultaneously redirect every real OpenAI call to the same host, with a key
// that endpoint never issued. Two OpenAI-compatible providers can only coexist
// if the URL travels with the client, not with the process.
//
// An empty baseURL falls back to NewOpenAIProviderWithModels' behaviour, so a
// misconfigured registry row degrades to plain OpenAI rather than to a request
// against "".
//
// name is the provider's canonical id, carried so that failures name the
// provider the user actually selected rather than the client that serves it.
func NewOpenAICompatibleProvider(apiKey, actorModel, criticModel, baseURL, name string) *OpenAIProvider {
	p := NewOpenAIProviderWithModels(apiKey, actorModel, criticModel)
	if v := strings.TrimRight(baseURL, "/"); v != "" {
		p.baseURL = v
	}
	if name != "" {
		p.name = name
	}
	return p
}

// LastCostUSD reports the USD cost of the most recent API call.
func (p *OpenAIProvider) LastCostUSD() float64 { return p.lastCost }

// LastUsage reports the input/output token counts of the most recent API call.
func (p *OpenAIProvider) LastUsage() (int64, int64) { return p.lastInput, p.lastOutput }

// --- wire types for chat/completions ---

type openaiMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openaiRequest struct {
	Model    string          `json:"model"`
	Messages []openaiMessage `json:"messages"`
	// The two output-ceiling fields are mutually exclusive and which one the API
	// accepts depends on the model — see isReasoningModel. Both are omitempty so
	// exactly one is ever sent.
	MaxTokens           int                   `json:"max_tokens,omitempty"`
	MaxCompletionTokens int                   `json:"max_completion_tokens,omitempty"`
	Temperature         *float64              `json:"temperature,omitempty"`
	ResponseFormat      *openaiResponseFormat `json:"response_format,omitempty"`
}

// openaiResponseFormat forces the wire-level JSON syntax check that a cheap
// or free-tier model otherwise skips by rambling in prose instead of
// answering. It's a request to the endpoint, not a prompt instruction, so it
// holds even when the model would ignore "respond only with JSON". Every
// caller of Complete already puts "JSON object" in its system prompt, which
// this mode requires be present somewhere in the messages.
type openaiResponseFormat struct {
	Type string `json:"type"`
}

type openaiResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
			// Refusal is populated instead of Content when the model declines.
			Refusal string `json:"refusal"`
			// Reasoning is a reasoning-family model's thinking, returned
			// alongside Content by OpenRouter — except some models leave
			// Content empty and put the whole answer here instead. See the
			// fallback below.
			Reasoning string `json:"reasoning"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int64 `json:"prompt_tokens"`
		CompletionTokens int64 `json:"completion_tokens"`
	} `json:"usage"`
	Error struct {
		Message string `json:"message"`
		Type    string `json:"type"`
		Code    string `json:"code"`
	} `json:"error"`
}

// isReasoningModel reports whether a model belongs to the reasoning families,
// which differ from the older chat models in two ways that both produce a hard
// 400 if got wrong: they require max_completion_tokens rather than max_tokens,
// and they reject any temperature other than the default.
//
// This is a property of the model, not a preference, so it is derived rather
// than configured — an operator adding "gpt-5.1" on the Models page must not
// have to know which spelling of the token ceiling it wants.
func isReasoningModel(model string) bool {
	m := strings.ToLower(model)
	for _, prefix := range []string{"gpt-5", "o1", "o3", "o4"} {
		if strings.HasPrefix(m, prefix) {
			return true
		}
	}
	return false
}

// chat issues one chat/completions call and returns the text plus finish reason.
func (p *OpenAIProvider) chat(ctx context.Context, model, system, user string, maxTokens int, jsonMode bool) (string, string, error) {
	reqBody := openaiRequest{
		Model: model,
		Messages: []openaiMessage{
			{Role: "system", Content: system},
			{Role: "user", Content: user},
		},
	}
	if jsonMode {
		reqBody.ResponseFormat = &openaiResponseFormat{Type: "json_object"}
	}
	if isReasoningModel(model) {
		// Reasoning tokens are drawn from this same ceiling, so it has to cover
		// the thinking as well as the answer — the same constraint the Anthropic
		// provider documents for adaptive thinking.
		reqBody.MaxCompletionTokens = maxTokens
	} else {
		reqBody.MaxTokens = maxTokens
		temp := 0.2
		reqBody.Temperature = &temp
	}

	b, err := json.Marshal(reqBody)
	if err != nil {
		return "", "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/chat/completions", bytes.NewReader(b))
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.apiKey)

	resp, err := p.http.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("%s request failed: %w", p.name, err)
	}
	defer resp.Body.Close()

	body, err := readAPIBody(ctx, p.name, resp)
	if err != nil {
		return "", "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// The body may echo the model and prompt but never the key, which is a
		// header. Include it so quota and bad-model errors stay actionable —
		// Classify reads these strings to categorise the failure.
		return "", "", fmt.Errorf("%s API returned %d: %s", p.name, resp.StatusCode, string(body))
	}

	var or openaiResponse
	if err := decodeAPIBody(p.name, resp.StatusCode, body, &or); err != nil {
		return "", "", err
	}
	p.lastInput = or.Usage.PromptTokens
	p.lastOutput = or.Usage.CompletionTokens
	p.lastCost = ModelCostUSD(model, p.lastInput, p.lastOutput)

	// A 200 carrying an error object is rare but real on compatible endpoints;
	// treating it as an empty answer would strand the caller with "no choices".
	if or.Error.Message != "" {
		return "", "", fmt.Errorf("%s API error: %s", p.name, or.Error.Message)
	}
	if len(or.Choices) == 0 {
		return "", "", fmt.Errorf("%s returned no choices", p.name)
	}
	choice := or.Choices[0]
	if choice.Message.Refusal != "" {
		return "", "refusal", nil
	}
	answer := choice.Message.Content
	if answer == "" {
		answer = choice.Message.Reasoning
	}
	return answer, choice.FinishReason, nil
}

// GetCodeEdit is the Actor: propose the complete corrected file.
func (p *OpenAIProvider) GetCodeEdit(ctx context.Context, task, fileName, codeContent, buildOutput string) (string, error) {
	system := "You are an expert software engineer acting as the Actor in an automated fix loop. " +
		"Infer the programming language and its conventions from the file name and contents. " +
		"Given a failing file and its build/test output, make the SMALLEST change that makes the tests pass. " +
		"Do not refactor unrelated code. Respond with the COMPLETE corrected file inside a single fenced code block."

	user := fmt.Sprintf("Task: %s\n\nFile: %s\n\nCurrent contents:\n```\n%s\n```\n\nBuild/test output:\n%s",
		task, fileName, codeContent, buildOutput)

	text, finish, err := p.chat(ctx, p.actorModel, system, user, 16000, false)
	if err != nil {
		return "", fmt.Errorf("%s actor request failed: %w", p.name, err)
	}
	if finish == "refusal" || finish == "content_filter" {
		return "", fmt.Errorf("actor request refused by safety classifier (%s)", finish)
	}
	return extractCode(text), nil
}

// ReviewEdit is the Critic: judge the proposed change before it is applied.
func (p *OpenAIProvider) ReviewEdit(ctx context.Context, task, fileName, oldContent, newContent, buildOutput string) (Verdict, error) {
	system := "You are the Critic in an automated fix loop. Review the proposed change for correctness and safety. " +
		"Approve only if it is a plausible, safe fix for the stated task. " +
		`Respond ONLY with a JSON object: {"approved": bool, "reasons": string}.`

	user := fmt.Sprintf("Task: %s\n\nFile: %s\n\nOriginal:\n```\n%s\n```\n\nProposed:\n```\n%s\n```\n\nBuild/test output that motivated the change:\n%s",
		task, fileName, oldContent, newContent, buildOutput)

	// The Critic's ceiling is larger here than the other providers' 2000 because
	// a reasoning model spends this budget on thinking before it writes the
	// verdict, and a Critic that runs out of budget mid-thought returns nothing —
	// which parseVerdict reads as a rejection, silently stalling the loop.
	text, finish, err := p.chat(ctx, p.criticModel, system, user, 8000, false)
	if err != nil {
		return Verdict{}, fmt.Errorf("%s critic request failed: %w", p.name, err)
	}
	if finish == "refusal" || finish == "content_filter" {
		return Verdict{Approved: false, Reasons: "critic refused to review (safety classifier)"}, nil
	}
	return parseVerdict(text), nil
}

// SelectMetric is the metric selector: pick at most one of an org's
// configured telemetry metrics as relevant to a merged task's intent.
// Mirrors ReviewEdit's call shape (same p.chat helper) rather than the
// agentic Complete path, since this is a short-list classification, not
// task decomposition. The token ceiling matches ReviewEdit's (not the
// other providers' 500) for the same reason documented there: a reasoning
// model spends this budget on thinking before it writes the verdict, and
// running out mid-thought returns nothing, which parseMetricSelection
// would read as an error rather than "no metric relevant."
func (p *OpenAIProvider) SelectMetric(ctx context.Context, intent string, options []MetricOption) (string, error) {
	if len(options) == 0 {
		return "", nil
	}
	system := "You are picking which configured telemetry metric (if any) is relevant to verify a code change. " +
		"Only choose a metric that plausibly measures the effect of the described change. " +
		`Respond ONLY with a JSON object: {"metric_name": string, "reason": string}. Use an empty metric_name if none of the options are relevant.`
	optionsText := ""
	for _, o := range options {
		optionsText += "- " + o.Name
		if o.Description != "" {
			optionsText += ": " + o.Description
		}
		optionsText += "\n"
	}
	user := fmt.Sprintf("Task: %s\n\nConfigured metrics:\n%s", intent, optionsText)

	text, finish, err := p.chat(ctx, p.criticModel, system, user, 8000, false)
	if err != nil {
		return "", fmt.Errorf("%s select metric request failed: %w", p.name, err)
	}
	if finish == "refusal" || finish == "content_filter" {
		return "", nil
	}
	sel, err := parseMetricSelection(text)
	if err != nil {
		return "", err
	}
	return sel.MetricName, nil
}

// Complete runs a single-turn (system + user) completion and returns the raw
// response text, satisfying the planner's Completer interface. Unlike
// GetCodeEdit it does not extract a fenced code block — callers parse their own
// structured output.
func (p *OpenAIProvider) Complete(ctx context.Context, system, user string) (string, error) {
	budget := CompletionBudget()
	text, finish, err := p.chat(ctx, p.actorModel, system, user, budget, true)
	if err != nil {
		return "", fmt.Errorf("%s completion request failed: %w", p.name, err)
	}
	if finish == "refusal" || finish == "content_filter" {
		return "", fmt.Errorf("completion refused by safety classifier (%s)", finish)
	}
	// "length" means the model was cut off mid-answer. Returning the partial text
	// would surface downstream as whatever the caller's parser complains about —
	// "unexpected end of JSON input" — which describes the symptom and hides the
	// cause. Name the cause here instead, as the other providers do.
	if finish == "length" {
		return "", &ErrTruncated{Budget: budget, Model: p.actorModel}
	}
	return text, nil
}

type openaiEmbedRequest struct {
	Model string `json:"model"`
	Input string `json:"input"`
}

type openaiEmbedResponse struct {
	Data []struct {
		Embedding []float32 `json:"embedding"`
	} `json:"data"`
}

// Embed returns a vector for text, so an org that connected only OpenAI can
// still use the learnings search that backs planner context.
func (p *OpenAIProvider) Embed(ctx context.Context, text string) ([]float32, error) {
	b, err := json.Marshal(openaiEmbedRequest{Model: "text-embedding-3-small", Input: text})
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/embeddings", bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.apiKey)

	resp, err := p.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s embed request failed: %w", p.name, err)
	}
	defer resp.Body.Close()

	body, err := readAPIBody(ctx, p.name+" embed", resp)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("%s embed API returned %d: %s", p.name, resp.StatusCode, string(body))
	}

	var er openaiEmbedResponse
	if err := decodeAPIBody(p.name+" embed", resp.StatusCode, body, &er); err != nil {
		return nil, err
	}
	if len(er.Data) == 0 || len(er.Data[0].Embedding) == 0 {
		return nil, fmt.Errorf("%s returned empty embedding", p.name)
	}
	return er.Data[0].Embedding, nil
}
