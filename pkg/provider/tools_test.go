package provider

import (
	"context"
	"encoding/json"
	"math"
	"testing"
)

func TestToolUsageAdd(t *testing.T) {
	var u ToolUsage
	u.Add(ToolUsage{InputTokens: 10, OutputTokens: 5, CacheReadTokens: 100, CacheWriteTokens: 20, CostUSD: 0.5})
	u.Add(ToolUsage{InputTokens: 1, OutputTokens: 2, CacheReadTokens: 3, CacheWriteTokens: 4, CostUSD: 0.25})

	want := ToolUsage{InputTokens: 11, OutputTokens: 7, CacheReadTokens: 103, CacheWriteTokens: 24, CostUSD: 0.75}
	if u != want {
		t.Fatalf("accumulated usage = %+v, want %+v", u, want)
	}
}

// A cached tool round is the whole reason cache classes exist: pricing cache
// reads as ordinary input is what would overstate a session run by roughly an
// order of magnitude and trip the per-job budget cap on a cheap job.
func TestCachedInputIsFarCheaperThanUncachedInput(t *testing.T) {
	const model = "claude-sonnet-5"
	const tokens = 1_000_000

	uncached := ModelCostUSD(model, tokens, 0)
	cached := ModelCostUSDWithCache(model, 0, 0, tokens, 0)

	if cached >= uncached {
		t.Fatalf("cache read (%.4f) should cost less than full input (%.4f)", cached, uncached)
	}
	// Anthropic's published ratio is a tenth; assert the derivation rather than
	// an absolute, so a PricingMap edit does not silently break the relation.
	if got, want := cached, uncached*anthropicCacheReadRatio; math.Abs(got-want) > 1e-9 {
		t.Fatalf("cache read cost = %.6f, want %.6f", got, want)
	}
}

func TestCacheWriteCostsMoreThanPlainInput(t *testing.T) {
	const model = "claude-sonnet-5"
	const tokens = 1_000_000

	plain := ModelCostUSD(model, tokens, 0)
	write := ModelCostUSDWithCache(model, 0, 0, 0, tokens)
	if write <= plain {
		t.Fatalf("cache write (%.4f) should cost more than plain input (%.4f)", write, plain)
	}
}

// An explicit entry must win over the derived ratio — that is the whole point
// of the fields being on Pricing rather than hard-coded.
func TestExplicitCacheRatesOverrideDerivedOnes(t *testing.T) {
	const model = "kiwi-test-explicit-cache"
	PricingMap[model] = Pricing{InputCostPerM: 10, OutputCostPerM: 20, CacheReadPerM: 7, CacheWritePerM: 9}
	defer delete(PricingMap, model)

	read, write := cacheRates(model)
	if read != 7 || write != 9 {
		t.Fatalf("cacheRates = (%v, %v), want (7, 9)", read, write)
	}
}

// A model with no PricingMap entry must still price its cache classes off its
// own family's input rate, never another provider's — the same guarantee
// ModelCostUSD already makes.
func TestDerivedCacheRatesFollowProviderFamily(t *testing.T) {
	geminiRead, _ := cacheRates("gemini-flash-latest")
	anthropicRead, _ := cacheRates("claude-sonnet-5")
	if geminiRead >= anthropicRead {
		t.Fatalf("gemini cache read (%v) should be cheaper than anthropic's (%v)", geminiRead, anthropicRead)
	}
	if geminiRead <= 0 || anthropicRead <= 0 {
		t.Fatalf("derived cache rates must be positive, got gemini=%v anthropic=%v", geminiRead, anthropicRead)
	}
}

// ModelCostUSDWithCache must not double-count: the input argument is the tokens
// billed at the full rate, and the cache classes are reported separately by
// every provider that offers them.
func TestCacheCostIsAdditiveNotOverlapping(t *testing.T) {
	const model = "claude-sonnet-5"
	split := ModelCostUSDWithCache(model, 100, 10, 900, 0)
	inputOnly := ModelCostUSD(model, 100, 10)
	readOnly := ModelCostUSDWithCache(model, 0, 0, 900, 0)
	if math.Abs(split-(inputOnly+readOnly)) > 1e-9 {
		t.Fatalf("combined = %.9f, want %.9f", split, inputOnly+readOnly)
	}
}

func TestAnthropicToolsCarrySchemaAndDescription(t *testing.T) {
	got := anthropicTools([]ToolDef{{
		Name:        "read_file",
		Description: "Read a file",
		Properties:  map[string]any{"path": map[string]any{"type": "string"}},
		Required:    []string{"path"},
	}})

	if len(got) != 1 || got[0].OfTool == nil {
		t.Fatalf("expected one custom tool, got %+v", got)
	}
	tool := got[0].OfTool
	if tool.Name != "read_file" {
		t.Errorf("name = %q", tool.Name)
	}
	if tool.Description.Value != "Read a file" {
		t.Errorf("description = %q", tool.Description.Value)
	}
	if len(tool.InputSchema.Required) != 1 || tool.InputSchema.Required[0] != "path" {
		t.Errorf("required = %v", tool.InputSchema.Required)
	}
}

// An empty description must not be sent as an empty string — the field is
// optional and a blank one is worse than absent for tool selection.
func TestAnthropicToolsOmitEmptyDescription(t *testing.T) {
	got := anthropicTools([]ToolDef{{Name: "noop"}})
	if got[0].OfTool.Description.Valid() {
		t.Fatalf("expected description to be omitted, got %q", got[0].OfTool.Description.Value)
	}
}

func TestAnthropicProviderIsAToolRunner(t *testing.T) {
	p := NewAnthropicProviderWithModels("k", "claude-sonnet-5", "claude-sonnet-5")
	if _, ok := AsToolRunner(p); !ok {
		t.Fatal("AnthropicProvider must satisfy ToolRunner")
	}
}

// A provider without tool support must be detectable rather than panic at the
// first Send — the caller falls back to the single-file loop.
func TestNonToolProviderIsNotAToolRunner(t *testing.T) {
	if _, ok := AsToolRunner(NewMockProvider()); ok {
		t.Fatal("MockProvider must not satisfy ToolRunner")
	}
}

func TestSendRequiresContent(t *testing.T) {
	p := NewAnthropicProviderWithModels("k", "claude-sonnet-5", "claude-sonnet-5")
	conv := p.StartConversation("sys", nil, ConversationOpts{})
	if _, err := conv.Send(context.Background(), "", nil); err == nil {
		t.Fatal("expected an error when Send is given neither text nor results")
	}
}

func TestMockToolRunnerDrivesATurnSequence(t *testing.T) {
	runner := &MockToolRunner{
		CostPerTurn: 0.01,
		Script: func(n int, text string, results []ToolResult) (Turn, error) {
			if n == 1 {
				return Turn{Calls: []ToolCall{MockCall("c1", "read_file", map[string]string{"path": "a.go"})}}, nil
			}
			return Turn{Text: "finished"}, nil
		},
	}
	conv := runner.StartConversation("sys", []ToolDef{{Name: "read_file"}}, ConversationOpts{})

	first, err := conv.Send(context.Background(), "go", nil)
	if err != nil {
		t.Fatalf("first send: %v", err)
	}
	if first.Done || len(first.Calls) != 1 {
		t.Fatalf("expected one tool call, got %+v", first)
	}
	var args struct {
		Path string `json:"path"`
	}
	if err := json.Unmarshal(first.Calls[0].Input, &args); err != nil {
		t.Fatalf("decode tool input: %v", err)
	}
	if args.Path != "a.go" {
		t.Errorf("path = %q", args.Path)
	}

	second, err := conv.Send(context.Background(), "", []ToolResult{{CallID: "c1", Content: "package main"}})
	if err != nil {
		t.Fatalf("second send: %v", err)
	}
	if !second.Done {
		t.Fatal("expected the conversation to end when no tools are requested")
	}
	if conv.Turns() != 2 {
		t.Errorf("turns = %d, want 2", conv.Turns())
	}
	if got := conv.Usage().CostUSD; math.Abs(got-0.02) > 1e-9 {
		t.Errorf("cost = %v, want 0.02", got)
	}
	if len(runner.Started) != 1 || runner.Started[0].System != "sys" {
		t.Errorf("StartConversation not recorded: %+v", runner.Started)
	}
}
