package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Client handles communication with the Kiwi Control Plane.
type Client struct {
	baseURL string
	http    *http.Client
}

// NewClient creates a new daemon API client.
func NewClient(baseURL string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		http: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// Heartbeat polls the Control Plane for new tasks.
// Returns a WorkerSpec payload if available, nil if no content, or an error.
func (c *Client) Heartbeat(ctx context.Context, req HeartbeatReq) (*HeartbeatRes, error) {
	buf, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal heartbeat request: %w", err)
	}

	url := c.baseURL + "/api/v1/daemon/heartbeat"

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(buf))
	if err != nil {
		return nil, fmt.Errorf("create heartbeat request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("heartbeat request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNoContent {
		// No tasks available
		return nil, nil
	}

	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("heartbeat failed with status %s: %s", resp.Status, strings.TrimSpace(string(msg)))
	}

	var res HeartbeatRes
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return nil, fmt.Errorf("decode heartbeat response: %w", err)
	}

	return &res, nil
}
