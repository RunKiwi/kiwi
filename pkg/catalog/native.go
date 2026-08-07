package catalog

import (
	"context"
	"strings"

	"github.com/ibreakthecloud/kiwi/pkg/provider"
)

// anthropicVersion is required on every Anthropic API request; the API rejects
// requests that omit it.
const anthropicVersion = "2023-06-01"

// nativeCapability records what is known about a native provider's model
// families: which id prefixes can call tools, and the smallest context window
// any model in that family ships with.
//
// It is a static table because the OpenAI and Anthropic list endpoints return
// nothing but ids — there is no capability field to read, and without a context
// length no native model could clear the selectability floor, which would empty
// the picker for every BYOK user.
//
// Prefixes rather than exact ids so a point release does not silently become
// unusable. Context values are deliberately the conservative floor for the
// family rather than the largest variant's: understating it can only make a
// model non-selectable, while overstating it would offer one the loop cannot
// actually fit a task into. A model matching no prefix keeps unknown capability
// and is simply not offered — a far better failure than one that dies mid-task
// on a parse error.
type nativeCap struct {
	prefix        string
	contextLength int
}

var nativeCapability = map[string][]nativeCap{
	provider.ProviderOpenAI: {
		{"gpt-4.1", 1_000_000}, {"gpt-4o", 128_000}, {"gpt-4", 128_000},
		{"gpt-5", 400_000}, {"chatgpt", 128_000},
		{"o1", 200_000}, {"o3", 200_000}, {"o4", 200_000},
	},
	provider.ProviderAnthropic: {{"claude-", 200_000}},
	provider.ProviderGemini:    {{"gemini-", 1_000_000}},
}

// OpenAILister reads the OpenAI model list, which returns ids and nothing else.
type OpenAILister struct{}

func (OpenAILister) List(ctx context.Context, endpoint, apiKey string) ([]DiscoveredModel, error) {
	var body struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := getJSON(ctx, endpoint, map[string]string{
		"Authorization": "Bearer " + apiKey,
	}, &body); err != nil {
		return nil, err
	}
	out := make([]DiscoveredModel, 0, len(body.Data))
	for _, m := range body.Data {
		out = append(out, DiscoveredModel{ID: m.ID})
	}
	return out, nil
}

// AnthropicLister reads the Anthropic model list.
type AnthropicLister struct{}

func (AnthropicLister) List(ctx context.Context, endpoint, apiKey string) ([]DiscoveredModel, error) {
	var body struct {
		Data []struct {
			ID          string `json:"id"`
			DisplayName string `json:"display_name"`
		} `json:"data"`
	}
	if err := getJSON(ctx, endpoint, map[string]string{
		"x-api-key":         apiKey,
		"anthropic-version": anthropicVersion,
	}, &body); err != nil {
		return nil, err
	}
	out := make([]DiscoveredModel, 0, len(body.Data))
	for _, m := range body.Data {
		out = append(out, DiscoveredModel{ID: m.ID, DisplayName: m.DisplayName})
	}
	return out, nil
}

// GeminiLister reads the Gemini model list. Unlike the other two natives it
// reports a token limit and which generation methods a model supports, so its
// rows need less guessing.
type GeminiLister struct{}

func (GeminiLister) List(ctx context.Context, endpoint, apiKey string) ([]DiscoveredModel, error) {
	var body struct {
		Models []struct {
			Name            string   `json:"name"`
			DisplayName     string   `json:"displayName"`
			InputTokenLimit int      `json:"inputTokenLimit"`
			Methods         []string `json:"supportedGenerationMethods"`
		} `json:"models"`
	}
	// Gemini authenticates with a query parameter rather than a header.
	sep := "?"
	if strings.Contains(endpoint, "?") {
		sep = "&"
	}
	if err := getJSON(ctx, endpoint+sep+"key="+apiKey, nil, &body); err != nil {
		return nil, err
	}
	out := make([]DiscoveredModel, 0, len(body.Models))
	for _, m := range body.Models {
		d := DiscoveredModel{
			// "models/gemini-2.0-flash" is a resource path; the model id is the
			// last segment. Storing the path would make every catalog lookup miss.
			ID:          strings.TrimPrefix(m.Name, "models/"),
			DisplayName: m.DisplayName,
		}
		if m.InputTokenLimit > 0 {
			d.ContextLength = ptrI(m.InputTokenLimit)
		}
		// Only a model that generates content is a chat model. Without this,
		// embedding and TTS models reach the picker and fail mid-task.
		if hasParameter(m.Methods, "generateContent") {
			d.Modality = "text->text"
		}
		out = append(out, d)
	}
	return out, nil
}

// ListerFor returns the discovery client for a provider. Every registry row
// must have one, or its models can never be discovered — asserted by
// TestListerForEveryRegistryProvider.
func ListerFor(providerID string) (Lister, bool) {
	switch providerID {
	case provider.ProviderOpenRouter:
		return OpenRouterLister{}, true
	case provider.ProviderOpenAI:
		return OpenAILister{}, true
	case provider.ProviderAnthropic:
		return AnthropicLister{}, true
	case provider.ProviderGemini:
		return GeminiLister{}, true
	}
	return nil, false
}

// EnrichFromPricingMap fills in what a native provider's list endpoint does not
// report: price, tool capability, and modality.
//
// Pricing is read from provider.PricingMap by EXACT id only. ModelCostUSD's
// fallback chain is deliberately not applied: guessing that an unrecognised
// model costs the same as gemini-2.0-flash is acceptable for reporting a bill
// after the fact, but here the number decides whether Kiwi funds the model, and
// a wrong guess spends real money. An unpriced model stays nil-priced, lands in
// TierUnknown, and is never Kiwi-funded.
//
// Fields a lister already supplied are never overwritten — a real value from
// the provider always beats a table lookup.
func EnrichFromPricingMap(providerID string, d *DiscoveredModel) {
	if p, ok := provider.PricingMap[d.ID]; ok {
		if d.InputCostPerM == nil {
			d.InputCostPerM = ptrF(p.InputCostPerM)
		}
		if d.OutputCostPerM == nil {
			d.OutputCostPerM = ptrF(p.OutputCostPerM)
		}
	}
	lower := strings.ToLower(d.ID)
	for _, cap := range nativeCapability[providerID] {
		if !strings.HasPrefix(lower, cap.prefix) {
			continue
		}
		if d.SupportsTools == nil {
			d.SupportsTools = ptrB(true)
		}
		if d.ContextLength == nil {
			d.ContextLength = ptrI(cap.contextLength)
		}
		break
	}
	if d.Modality == "" && d.SupportsTools != nil && *d.SupportsTools {
		d.Modality = "text->text"
	}
}
