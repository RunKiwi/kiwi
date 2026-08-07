package catalog

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

// tokensPerMillion converts OpenRouter's per-token prices to the per-million
// unit the catalog and PricingMap both use.
const tokensPerMillion = 1_000_000

// OpenRouterLister reads GET {base}/models. It is the only lister that returns
// pricing and tool support, which is what lets a discovered model be priced,
// tiered, and funded without a second source.
type OpenRouterLister struct{}

type openRouterResponse struct {
	Data []struct {
		ID           string `json:"id"`
		Name         string `json:"name"`
		ContextLen   int    `json:"context_length"`
		Architecture struct {
			Modality string `json:"modality"`
		} `json:"architecture"`
		Pricing struct {
			Prompt     string `json:"prompt"`
			Completion string `json:"completion"`
		} `json:"pricing"`
		SupportedParameters []string `json:"supported_parameters"`
	} `json:"data"`
}

func (OpenRouterLister) List(ctx context.Context, baseURL, apiKey string) ([]DiscoveredModel, error) {
	url := strings.TrimRight(baseURL, "/") + "/models"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	// The endpoint is public, but sending the key when we have one keeps the
	// response scoped to what this account can actually call.
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("openrouter models: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("openrouter models: status %d", resp.StatusCode)
	}

	var body openRouterResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("openrouter models: decode: %w", err)
	}

	out := make([]DiscoveredModel, 0, len(body.Data))
	for _, m := range body.Data {
		d := DiscoveredModel{
			ID:             m.ID,
			DisplayName:    m.Name,
			Modality:       m.Architecture.Modality,
			InputCostPerM:  parsePerTokenPrice(m.Pricing.Prompt),
			OutputCostPerM: parsePerTokenPrice(m.Pricing.Completion),
			SupportsTools:  ptrB(hasParameter(m.SupportedParameters, "tools")),
		}
		if m.ContextLen > 0 {
			d.ContextLength = ptrI(m.ContextLen)
		}
		out = append(out, d)
	}
	return out, nil
}

// parsePerTokenPrice converts a per-token USD string to USD per million.
//
// An unparseable or negative value returns nil, not zero. That distinction is
// load-bearing: nil puts the model in tier "unknown" where Kiwi will not fund
// it, whereas zero would advertise a paid model as free.
func parsePerTokenPrice(s string) *float64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil || v < 0 {
		return nil
	}
	return ptrF(v * tokensPerMillion)
}

func hasParameter(params []string, want string) bool {
	for _, p := range params {
		if p == want {
			return true
		}
	}
	return false
}
