package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
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
		http:        http.DefaultClient,
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
func (p *GeminiProvider) generate(ctx context.Context, model, system, user string, maxTokens int) (string, string, error) {
	reqBody := geminiRequest{
		SystemInstruction: &geminiContent{Parts: []geminiPart{{Text: system}}},
		Contents:          []geminiContent{{Role: "user", Parts: []geminiPart{{Text: user}}}},
	}
	reqBody.GenerationConfig.MaxOutputTokens = maxTokens
	reqBody.GenerationConfig.Temperature = 0.2

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

	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// The error body may echo the model/prompt but not the key (which is a
		// header). Include it to surface actionable errors (quota, bad model).
		return "", "", fmt.Errorf("gemini API returned %d: %s", resp.StatusCode, string(body))
	}

	var gr geminiResponse
	if err := json.Unmarshal(body, &gr); err != nil {
		return "", "", fmt.Errorf("decode gemini response: %w", err)
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
	system := "You are an expert Go engineer acting as the Actor in an automated fix loop. " +
		"Given a failing file and its build/test output, make the SMALLEST change that makes the tests pass. " +
		"Do not refactor unrelated code. Respond with the COMPLETE corrected file inside a single fenced code block."

	user := fmt.Sprintf("Task: %s\n\nFile: %s\n\nCurrent contents:\n```\n%s\n```\n\nBuild/test output:\n%s",
		task, fileName, codeContent, buildOutput)

	text, finish, err := p.generate(ctx, p.actorModel, system, user, 8192)
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

	text, finish, err := p.generate(ctx, p.criticModel, system, user, 2000)
	if err != nil {
		return Verdict{}, fmt.Errorf("gemini critic request failed: %w", err)
	}
	if finish == "SAFETY" || finish == "PROHIBITED_CONTENT" {
		return Verdict{Approved: false, Reasons: "critic blocked by safety filter"}, nil
	}
	return parseVerdict(text), nil
}
