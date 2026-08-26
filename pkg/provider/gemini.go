package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// defaultGeminiBaseURL is Google's Generative Language API. Overridable per
// provider for tests.
const defaultGeminiBaseURL = "https://generativelanguage.googleapis.com/v1beta"

// GeminiProvider is a live Gemini-backed Actor and Critic. It talks to the
// Generative Language API directly (a thin JSON client) rather than pulling in
// an SDK, mirroring the small HTTP clients already used elsewhere in the tree.
type GeminiProvider struct {
	apiKey      string
	actorModel  string
	criticModel string
	baseURL     string
	http        *http.Client

	lastCost   float64
	lastInput  int64
	lastOutput int64
}

// NewGeminiProviderWithModels builds a provider with customized Actor and Critic
// models (e.g. "gemini-2.0-flash"). An empty model defaults to gemini-2.0-flash.
func NewGeminiProviderWithModels(apiKey, actorModel, criticModel string) *GeminiProvider {
	if actorModel == "" {
		actorModel = "gemini-2.0-flash"
	}
	if criticModel == "" {
		criticModel = "gemini-2.0-flash"
	}
	return &GeminiProvider{
		apiKey:      apiKey,
		actorModel:  actorModel,
		criticModel: criticModel,
		baseURL:     defaultGeminiBaseURL,
		http:        retryingClient(),
	}
}

// LastCostUSD reports the USD cost of the most recent API call.
func (p *GeminiProvider) LastCostUSD() float64 { return p.lastCost }

// LastUsage reports the input/output token counts of the most recent API call.
func (p *GeminiProvider) LastUsage() (int64, int64) { return p.lastInput, p.lastOutput }

// --- wire types for generateContent ---

type geminiPart struct {
	Text string `json:"text"`
}

type geminiContent struct {
	Role  string       `json:"role,omitempty"`
	Parts []geminiPart `json:"parts"`
}

type geminiRequest struct {
	SystemInstruction *geminiContent  `json:"systemInstruction,omitempty"`
	Contents          []geminiContent `json:"contents"`
	GenerationConfig  struct {
		MaxOutputTokens int     `json:"maxOutputTokens,omitempty"`
		Temperature     float64 `json:"temperature,omitempty"`
		// ResponseMIMEType forces syntactically valid JSON at the wire level,
		// so a cheap/free model that would otherwise ramble in prose instead
		// of answering can't — the constraint holds even when it would ignore
		// a prompt instruction to respond with JSON only.
		ResponseMIMEType string `json:"responseMimeType,omitempty"`
	} `json:"generationConfig"`
}

type geminiResponse struct {
	Candidates []struct {
		Content      geminiContent `json:"content"`
		FinishReason string        `json:"finishReason"`
	} `json:"candidates"`
	UsageMetadata struct {
		PromptTokenCount     int64 `json:"promptTokenCount"`
		CandidatesTokenCount int64 `json:"candidatesTokenCount"`
	} `json:"usageMetadata"`
	// PromptFeedback carries a block reason when the request itself is refused.
	PromptFeedback struct {
		BlockReason string `json:"blockReason"`
	} `json:"promptFeedback"`
}

// generate issues one generateContent call and returns the concatenated text.
func (p *GeminiProvider) generate(ctx context.Context, model, system, user string, maxTokens int, jsonMode bool) (string, string, error) {
	reqBody := geminiRequest{
		SystemInstruction: &geminiContent{Parts: []geminiPart{{Text: system}}},
		Contents:          []geminiContent{{Role: "user", Parts: []geminiPart{{Text: user}}}},
	}
	reqBody.GenerationConfig.MaxOutputTokens = maxTokens
	reqBody.GenerationConfig.Temperature = 0.2
	if jsonMode {
		reqBody.GenerationConfig.ResponseMIMEType = "application/json"
	}

	b, err := json.Marshal(reqBody)
	if err != nil {
		return "", "", err
	}

	url := fmt.Sprintf("%s/models/%s:generateContent", p.baseURL, model)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(b))
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Content-Type", "application/json")
	// Send the key as a header, not a query param, so it cannot leak into URL logs.
	req.Header.Set("x-goog-api-key", p.apiKey)

	resp, err := p.http.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("gemini request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := readAPIBody(ctx, "gemini", resp)
	if err != nil {
		return "", "", err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// The error body may echo the model/prompt but not the key (which is a
		// header). Include it to surface actionable errors (quota, bad model).
		return "", "", fmt.Errorf("gemini API returned %d: %s", resp.StatusCode, string(body))
	}

	var gr geminiResponse
	if err := decodeAPIBody("gemini", resp.StatusCode, body, &gr); err != nil {
		return "", "", err
	}
	p.lastInput = gr.UsageMetadata.PromptTokenCount
	p.lastOutput = gr.UsageMetadata.CandidatesTokenCount
	p.lastCost = ModelCostUSD(model, p.lastInput, p.lastOutput)

	if gr.PromptFeedback.BlockReason != "" {
		return "", gr.PromptFeedback.BlockReason, nil
	}
	if len(gr.Candidates) == 0 {
		return "", "", fmt.Errorf("gemini returned no candidates")
	}

	var text strings.Builder
	for _, part := range gr.Candidates[0].Content.Parts {
		text.WriteString(part.Text)
	}
	return text.String(), gr.Candidates[0].FinishReason, nil
}

// GetCodeEdit is the Actor: propose the complete corrected file.
func (p *GeminiProvider) GetCodeEdit(ctx context.Context, task, fileName, codeContent, buildOutput string) (string, error) {
	system := "You are an expert software engineer acting as the Actor in an automated fix loop. " +
		"Infer the programming language and its conventions from the file name and contents. " +
		"Given a failing file and its build/test output, make the SMALLEST change that makes the tests pass. " +
		"Do not refactor unrelated code. Respond with the COMPLETE corrected file inside a single fenced code block."

	user := fmt.Sprintf("Task: %s\n\nFile: %s\n\nCurrent contents:\n```\n%s\n```\n\nBuild/test output:\n%s",
		task, fileName, codeContent, buildOutput)

	text, finish, err := p.generate(ctx, p.actorModel, system, user, 8192, false)
	if err != nil {
		return "", fmt.Errorf("gemini actor request failed: %w", err)
	}
	if finish == "SAFETY" || finish == "PROHIBITED_CONTENT" {
		return "", fmt.Errorf("actor request blocked by safety filter (%s)", finish)
	}
	return extractCode(text), nil
}

// ReviewEdit is the Critic: judge the proposed change before it is applied.
func (p *GeminiProvider) ReviewEdit(ctx context.Context, task, fileName, oldContent, newContent, buildOutput string) (Verdict, error) {
	system := "You are the Critic in an automated fix loop. Review the proposed change for correctness and safety. " +
		"Approve only if it is a plausible, safe fix for the stated task. " +
		`Respond ONLY with a JSON object: {"approved": bool, "reasons": string}.`

	user := fmt.Sprintf("Task: %s\n\nFile: %s\n\nOriginal:\n```\n%s\n```\n\nProposed:\n```\n%s\n```\n\nBuild/test output that motivated the change:\n%s",
		task, fileName, oldContent, newContent, buildOutput)

	text, finish, err := p.generate(ctx, p.criticModel, system, user, 2000, false)
	if err != nil {
		return Verdict{}, fmt.Errorf("gemini critic request failed: %w", err)
	}
	if finish == "SAFETY" || finish == "PROHIBITED_CONTENT" {
		return Verdict{Approved: false, Reasons: "critic blocked by safety filter"}, nil
	}
	return parseVerdict(text), nil
}

// SelectMetric is the metric selector: pick at most one of an org's
// configured telemetry metrics as relevant to a merged task's intent.
// Mirrors ReviewEdit's call shape (same p.generate helper, low token
// ceiling, safety-filter handling) rather than the agentic Complete path,
// since this is a short-list classification, not task decomposition.
func (p *GeminiProvider) SelectMetric(ctx context.Context, intent string, options []MetricOption) (string, error) {
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

	text, finish, err := p.generate(ctx, p.criticModel, system, user, 500, false)
	if err != nil {
		return "", fmt.Errorf("gemini select metric request failed: %w", err)
	}
	if finish == "SAFETY" || finish == "PROHIBITED_CONTENT" {
		return "", nil
	}
	sel, err := parseMetricSelection(text)
	if err != nil {
		return "", err
	}
	return sel.MetricName, nil
}

// Complete is a general single-shot completion: given a system and user
// prompt, return the model's text response. Used for repo exploration and
// multi-file edits, which are not shaped like GetCodeEdit's single-file fix.
func (p *GeminiProvider) Complete(ctx context.Context, system, user string) (string, error) {
	budget := CompletionBudget()
	text, finish, err := p.generate(ctx, p.actorModel, system, user, budget, true)
	if err != nil {
		return "", fmt.Errorf("gemini complete request failed: %w", err)
	}
	if finish == "SAFETY" || finish == "PROHIBITED_CONTENT" {
		return "", fmt.Errorf("complete request blocked by safety filter (%s)", finish)
	}
	// MAX_TOKENS means the model was cut off mid-answer. Returning the partial
	// text lets it surface downstream as whatever the caller's parser happens to
	// complain about — "unexpected end of JSON input" — which describes the
	// symptom and hides the cause. Name the cause here instead.
	if finish == "MAX_TOKENS" {
		return "", &ErrTruncated{Budget: budget, Model: p.actorModel}
	}
	return text, nil
}

type geminiEmbedRequest struct {
	Model   string        `json:"model"`
	Content geminiContent `json:"content"`
}

type geminiEmbedResponse struct {
	Embedding struct {
		Values []float32 `json:"values"`
	} `json:"embedding"`
}

func (p *GeminiProvider) Embed(ctx context.Context, text string) ([]float32, error) {
	reqBody := geminiEmbedRequest{
		Model: "models/text-embedding-004",
		Content: geminiContent{
			Parts: []geminiPart{{Text: text}},
		},
	}

	b, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}

	url := fmt.Sprintf("%s/models/text-embedding-004:embedContent", p.baseURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-goog-api-key", p.apiKey)

	resp, err := p.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("gemini embed request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := readAPIBody(ctx, "gemini embed", resp)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("gemini embed API returned %d: %s", resp.StatusCode, string(body))
	}

	var gr geminiEmbedResponse
	if err := decodeAPIBody("gemini embed", resp.StatusCode, body, &gr); err != nil {
		return nil, err
	}

	if len(gr.Embedding.Values) == 0 {
		return nil, fmt.Errorf("gemini returned empty embedding")
	}

	return gr.Embedding.Values, nil
}
