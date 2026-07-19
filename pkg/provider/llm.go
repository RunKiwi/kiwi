package provider

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

// AnthropicProvider is a live Claude-backed Actor and Critic.
type AnthropicProvider struct {
	client      anthropic.Client
	actorModel  string
	criticModel string
	lastCost    float64
	lastInput   int64
	lastOutput  int64
}

// NewAnthropicProvider builds a provider using the given API key and default Opus models.
func NewAnthropicProvider(apiKey string) *AnthropicProvider {
	return NewAnthropicProviderWithModels(apiKey, "claude-opus-4-8", "claude-opus-4-8")
}

// NewAnthropicProviderWithModels builds a provider with customized Actor and Critic models.
func NewAnthropicProviderWithModels(apiKey, actorModel, criticModel string) *AnthropicProvider {
	if actorModel == "" {
		actorModel = "claude-opus-4-8"
	}
	if criticModel == "" {
		criticModel = "claude-opus-4-8"
	}
	return &AnthropicProvider{
		client:      anthropic.NewClient(option.WithAPIKey(apiKey)),
		actorModel:  actorModel,
		criticModel: criticModel,
	}
}

// LastCostUSD reports the USD cost of the most recent API call.
func (p *AnthropicProvider) LastCostUSD() float64 { return p.lastCost }

// LastUsage reports the input/output token counts of the most recent API call.
func (p *AnthropicProvider) LastUsage() (int64, int64) { return p.lastInput, p.lastOutput }

func (p *AnthropicProvider) recordCost(u anthropic.Usage, model string) {
	p.lastCost = ModelCostUSD(model, u.InputTokens, u.OutputTokens)
	p.lastInput = u.InputTokens
	p.lastOutput = u.OutputTokens
}

func collectText(resp *anthropic.Message) string {
	var b strings.Builder
	for _, block := range resp.Content {
		if t, ok := block.AsAny().(anthropic.TextBlock); ok {
			b.WriteString(t.Text)
		}
	}
	return b.String()
}

// GetCodeEdit is the Actor: propose the complete corrected file.
func (p *AnthropicProvider) GetCodeEdit(ctx context.Context, task, fileName, codeContent, buildOutput string) (string, error) {
	system := "You are an expert software engineer acting as the Actor in an automated fix loop. " +
		"Infer the programming language and its conventions from the file name and contents. " +
		"Given a failing file and its build/test output, make the SMALLEST change that makes the tests pass. " +
		"Do not refactor unrelated code. Respond with the COMPLETE corrected file inside a single fenced code block."

	user := fmt.Sprintf("Task: %s\n\nFile: %s\n\nCurrent contents:\n```\n%s\n```\n\nBuild/test output:\n%s",
		task, fileName, codeContent, buildOutput)

	adaptive := anthropic.ThinkingConfigAdaptiveParam{}
	resp, err := p.client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     anthropic.Model(p.actorModel),
		MaxTokens: 16000,
		System:    []anthropic.TextBlockParam{{Text: system}},
		Thinking:  anthropic.ThinkingConfigParamUnion{OfAdaptive: &adaptive},
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(user)),
		},
	})
	if err != nil {
		return "", fmt.Errorf("anthropic actor request failed: %w", err)
	}
	p.recordCost(resp.Usage, p.actorModel)
	if resp.StopReason == anthropic.StopReasonRefusal {
		return "", errors.New("actor request refused by safety classifier")
	}
	return extractCode(collectText(resp)), nil
}

// ReviewEdit is the Critic: judge the proposed change before it is applied.
func (p *AnthropicProvider) ReviewEdit(ctx context.Context, task, fileName, oldContent, newContent, buildOutput string) (Verdict, error) {
	system := "You are the Critic in an automated fix loop. Review the proposed change for correctness and safety. " +
		"Approve only if it is a plausible, safe fix for the stated task. " +
		`Respond ONLY with a JSON object: {"approved": bool, "reasons": string}.`

	user := fmt.Sprintf("Task: %s\n\nFile: %s\n\nOriginal:\n```\n%s\n```\n\nProposed:\n```\n%s\n```\n\nBuild/test output that motivated the change:\n%s",
		task, fileName, oldContent, newContent, buildOutput)

	adaptive := anthropic.ThinkingConfigAdaptiveParam{}
	resp, err := p.client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     anthropic.Model(p.criticModel),
		MaxTokens: 2000,
		System:    []anthropic.TextBlockParam{{Text: system}},
		Thinking:  anthropic.ThinkingConfigParamUnion{OfAdaptive: &adaptive},
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(user)),
		},
	})
	if err != nil {
		return Verdict{}, fmt.Errorf("anthropic critic request failed: %w", err)
	}
	p.recordCost(resp.Usage, p.criticModel)
	if resp.StopReason == anthropic.StopReasonRefusal {
		return Verdict{Approved: false, Reasons: "critic refused to review (safety classifier)"}, nil
	}
	return parseVerdict(collectText(resp)), nil
}

// Complete runs a single-turn (system + user) completion and returns the raw
// response text. Unlike GetCodeEdit it does not extract a fenced code block —
// callers such as the planner parse their own structured output. This satisfies
// the planner's Completer interface so LLMPlanner can decompose tasks with a
// live model.
func (p *AnthropicProvider) Complete(ctx context.Context, system, user string) (string, error) {
	adaptive := anthropic.ThinkingConfigAdaptiveParam{}
	resp, err := p.client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     anthropic.Model(p.actorModel),
		MaxTokens: 8000,
		System:    []anthropic.TextBlockParam{{Text: system}},
		Thinking:  anthropic.ThinkingConfigParamUnion{OfAdaptive: &adaptive},
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(user)),
		},
	})
	if err != nil {
		return "", fmt.Errorf("anthropic completion request failed: %w", err)
	}
	p.recordCost(resp.Usage, p.actorModel)
	if resp.StopReason == anthropic.StopReasonRefusal {
		return "", errors.New("completion refused by safety classifier")
	}
	return collectText(resp), nil
}
