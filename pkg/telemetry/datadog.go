package telemetry

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

type datadogProvider struct {
	baseURL string
	apiKey  string
	appKey  string
	client  *http.Client
}

// NewDatadogProvider defaults baseURL to Datadog's US1 site. A per-org site
// override (e.g. datadoghq.eu) is not in scope for this plan — every org
// configuring Datadog telemetry today is assumed US1; revisit if that stops
// being true.
func NewDatadogProvider(apiKey, appKey string) Provider {
	return &datadogProvider{
		baseURL: "https://api.datadoghq.com",
		apiKey:  apiKey,
		appKey:  appKey,
		client:  &http.Client{Timeout: 15 * time.Second},
	}
}

type datadogQueryResponse struct {
	Status string `json:"status"`
	Series []struct {
		Pointlist [][2]*float64 `json:"pointlist"`
	} `json:"series"`
}

func (p *datadogProvider) Query(ctx context.Context, query string, start, end time.Time) (Result, error) {
	u := fmt.Sprintf("%s/api/v1/query?query=%s&from=%d&to=%d",
		p.baseURL, url.QueryEscape(query), start.Unix(), end.Unix())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return Result{}, err
	}
	req.Header.Set("DD-API-KEY", p.apiKey)
	req.Header.Set("DD-APPLICATION-KEY", p.appKey)

	resp, err := p.client.Do(req)
	if err != nil {
		return Result{}, fmt.Errorf("datadog query: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<12))
		return Result{}, fmt.Errorf("datadog query returned %d: %s", resp.StatusCode, string(b))
	}

	var out datadogQueryResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return Result{}, fmt.Errorf("decode datadog response: %w", err)
	}
	if len(out.Series) == 0 {
		return Result{}, fmt.Errorf("datadog returned no series for query %q over [%s, %s]", query, start, end)
	}

	var sum float64
	count := 0
	for _, point := range out.Series[0].Pointlist {
		if len(point) != 2 || point[1] == nil {
			continue // a nil value marks a gap — must not be counted as zero
		}
		sum += *point[1]
		count++
	}
	if count == 0 {
		return Result{}, fmt.Errorf("datadog series for query %q had no non-null data points", query)
	}
	return Result{SampleCount: count, Mean: sum / float64(count)}, nil
}
