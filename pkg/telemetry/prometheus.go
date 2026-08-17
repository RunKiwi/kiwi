package telemetry

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

type prometheusProvider struct {
	baseURL string
	token   string
	client  *http.Client
}

// NewPrometheusProvider connects to a Prometheus-compatible /api/v1
// endpoint. baseURL and token come from an org's PROMETHEUS_BASE_URL and
// PROMETHEUS_BEARER_TOKEN credentials (see Task 6) — the base URL travels
// as a credential-shaped config value alongside the actual secret because
// there is no other per-org config channel to the daemon today, and the
// existing sealed-credential-bundle delivery already solves "get this to
// the daemon safely" for free.
func NewPrometheusProvider(baseURL, token string) Provider {
	return &prometheusProvider{baseURL: baseURL, token: token, client: &http.Client{Timeout: 15 * time.Second}}
}

type prometheusRangeResponse struct {
	Status string `json:"status"`
	Data   struct {
		ResultType string `json:"resultType"`
		Result     []struct {
			Values [][2]interface{} `json:"values"`
		} `json:"result"`
	} `json:"data"`
}

func (p *prometheusProvider) Query(ctx context.Context, query string, start, end time.Time) (Result, error) {
	u := fmt.Sprintf("%s/api/v1/query_range?query=%s&start=%d&end=%d&step=60",
		p.baseURL, url.QueryEscape(query), start.Unix(), end.Unix())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return Result{}, err
	}
	req.Header.Set("Authorization", "Bearer "+p.token)

	resp, err := p.client.Do(req)
	if err != nil {
		return Result{}, fmt.Errorf("prometheus query_range: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<12))
		return Result{}, fmt.Errorf("prometheus query_range returned %d: %s", resp.StatusCode, string(b))
	}

	var out prometheusRangeResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return Result{}, fmt.Errorf("decode prometheus response: %w", err)
	}
	if out.Status != "success" || len(out.Data.Result) == 0 {
		return Result{}, fmt.Errorf("prometheus returned no series for query %q over [%s, %s]", query, start, end)
	}

	values := out.Data.Result[0].Values
	if len(values) == 0 {
		return Result{}, fmt.Errorf("prometheus series for query %q has no data points", query)
	}

	var sum float64
	count := 0
	for _, v := range values {
		if len(v) != 2 {
			continue
		}
		s, ok := v[1].(string)
		if !ok {
			continue
		}
		f, err := strconv.ParseFloat(s, 64)
		if err != nil {
			continue
		}
		sum += f
		count++
	}
	if count == 0 {
		return Result{}, fmt.Errorf("prometheus series for query %q had no parseable data points", query)
	}
	return Result{SampleCount: count, Mean: sum / float64(count)}, nil
}
