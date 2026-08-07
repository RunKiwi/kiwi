package catalog

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// OpenAILister fetches the /v1/models list.
type OpenAILister struct{}

func (OpenAILister) List(ctx context.Context, baseURL, apiKey string) ([]DiscoveredModel, error) {
	var body struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	url := strings.TrimRight(baseURL, "/") + "/v1/models"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("openai models: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("openai models: status %d", resp.StatusCode)
	}

	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("openai models: decode: %w", err)
	}

	out := make([]DiscoveredModel, 0, len(body.Data))
	for _, m := range body.Data {
		out = append(out, DiscoveredModel{ID: m.ID})
	}
	return out, nil
}

// AnthropicLister fetches the /v1/models list.
type AnthropicLister struct{}

func (AnthropicLister) List(ctx context.Context, baseURL, apiKey string) ([]DiscoveredModel, error) {
	var body struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	url := strings.TrimRight(baseURL, "/") + "/v1/models"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("x-api-key", apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("anthropic models: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("anthropic models: status %d", resp.StatusCode)
	}

	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("anthropic models: decode: %w", err)
	}

	out := make([]DiscoveredModel, 0, len(body.Data))
	for _, m := range body.Data {
		out = append(out, DiscoveredModel{ID: m.ID})
	}
	return out, nil
}

// GeminiLister fetches the /v1beta/models list.
type GeminiLister struct{}

func (GeminiLister) List(ctx context.Context, baseURL, apiKey string) ([]DiscoveredModel, error) {
	var body struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	url := strings.TrimRight(baseURL, "/") + "/v1beta/models?key=" + apiKey
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("gemini models: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("gemini models: status %d", resp.StatusCode)
	}

	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("gemini models: decode: %w", err)
	}

	out := make([]DiscoveredModel, 0, len(body.Models))
	for _, m := range body.Models {
		// Gemini returns "models/gemini-1.5-pro", we only want the basename.
		id := strings.TrimPrefix(m.Name, "models/")
		out = append(out, DiscoveredModel{ID: id})
	}
	return out, nil
}

// EnrichFromPricingMap takes a raw list of models (typically from a native
// provider that only returns ids) and attaches prices from a static lookup.
//
// A static map is fragile, but since native providers omit pricing from their
// APIs, it is the only way a BYOK model can be tiered and offered.
func EnrichFromPricingMap(models []DiscoveredModel, lookup func(string) (*float64, *float64)) []DiscoveredModel {
	out := make([]DiscoveredModel, 0, len(models))
	for _, m := range models {
		in, outCost := lookup(m.ID)
		m.InputCostPerM = in
		m.OutputCostPerM = outCost
		out = append(out, m)
	}
	return out
}
