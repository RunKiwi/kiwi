package daemon

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/ibreakthecloud/kiwi/pkg/crypto"
)

// Client handles communication with the Kiwi Control Plane.
type Client struct {
	baseURL  string
	http     *http.Client
	signPriv ed25519.PrivateKey
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

// SetSigner installs the Ed25519 private key used to authenticate requests.
// Once set, every request body is signed and the signature is sent in the
// X-Kiwi-Signature header so the Control Plane can verify it against the
// daemon's registered public key.
func (c *Client) SetSigner(priv ed25519.PrivateKey) {
	c.signPriv = priv
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

	// Authenticate the request by signing the exact body bytes with the
	// daemon's Ed25519 identity key.
	if c.signPriv != nil {
		sig := crypto.Sign(c.signPriv, buf)
		httpReq.Header.Set("X-Kiwi-Signature", base64.StdEncoding.EncodeToString(sig))
	}

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
