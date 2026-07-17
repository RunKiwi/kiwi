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

// signedPost marshals body, signs the exact bytes with the daemon's Ed25519
// identity, and POSTs to path with the signature in X-Kiwi-Signature. It
// returns the response for the caller to interpret, having already handled
// transport errors. The caller owns closing the body.
func (c *Client) signedPost(ctx context.Context, path string, body any) (*http.Response, []byte, error) {
	buf, err := json.Marshal(body)
	if err != nil {
		return nil, nil, fmt.Errorf("marshal request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(buf))
	if err != nil {
		return nil, nil, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if c.signPriv != nil {
		sig := crypto.Sign(c.signPriv, buf)
		httpReq.Header.Set("X-Kiwi-Signature", base64.StdEncoding.EncodeToString(sig))
	}
	resp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, buf, fmt.Errorf("request failed: %w", err)
	}
	return resp, buf, nil
}

// Register performs the one-time join handshake: it presents a join token and
// the daemon's public keys, signed by the identity key. On success the daemon
// is bound to the token's org and may begin heartbeating. A 409 means the
// Control Plane already knows this identity with a different encryption key —
// the caller re-registers to rotate it.
func (c *Client) Register(ctx context.Context, req RegisterReq) error {
	resp, _, err := c.signedPost(ctx, "/api/v1/daemon/register", req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusCreated || resp.StatusCode == http.StatusOK {
		return nil
	}
	msg, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
	return fmt.Errorf("register failed with status %s: %s", resp.Status, strings.TrimSpace(string(msg)))
}

// Heartbeat polls the Control Plane for new tasks.
// Returns a WorkerSpec payload if available, nil if no content, or an error.
func (c *Client) Heartbeat(ctx context.Context, req HeartbeatReq) (*HeartbeatRes, error) {
	resp, _, err := c.signedPost(ctx, "/api/v1/daemon/heartbeat", req)
	if err != nil {
		return nil, err
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

// ReportResult reports a task's terminal outcome, presenting the lease fencing
// token so the Control Plane can close the lease. A 409 means the lease was
// reassigned (this daemon lost ownership) — the caller should drop the result.
func (c *Client) ReportResult(ctx context.Context, req ResultReq) error {
	resp, _, err := c.signedPost(ctx, "/api/v1/daemon/result", req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNoContent || resp.StatusCode == http.StatusOK {
		return nil
	}
	msg, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
	return fmt.Errorf("report result failed with status %s: %s", resp.Status, strings.TrimSpace(string(msg)))
}
