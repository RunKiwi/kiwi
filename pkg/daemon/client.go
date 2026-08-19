package daemon

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
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

// RenewLease extends a task's lease while it is still running, proving ongoing
// liveness so the Control Plane does not requeue it. A 409 means the lease was
// lost (e.g. expired and reassigned) — the caller should abort execution.
func (c *Client) RenewLease(ctx context.Context, req RenewReq) error {
	resp, _, err := c.signedPost(ctx, "/api/v1/daemon/renew", req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNoContent || resp.StatusCode == http.StatusOK {
		return nil
	}
	msg, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
	if resp.StatusCode == http.StatusConflict {
		// 409 is the Control Plane stating the task is no longer ours: cancelled,
		// reassigned after expiry, or already completed. Unlike a network error
		// this is definitive, so it is typed — the caller aborts the run on this
		// and only this, and keeps working through transient failures.
		return fmt.Errorf("%w: %s", ErrLeaseLost, strings.TrimSpace(string(msg)))
	}
	return fmt.Errorf("renew lease failed with status %s: %s", resp.Status, strings.TrimSpace(string(msg)))
}

// ReportProgress posts partial telemetry for a still-running task. It is
// best-effort: the caller logs and ignores any error, because a run must never
// fail on account of its own observability.
func (c *Client) ReportProgress(ctx context.Context, req ProgressReq) error {
	resp, _, err := c.signedPost(ctx, "/api/v1/daemon/progress", req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNoContent || resp.StatusCode == http.StatusOK {
		return nil
	}
	msg, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
	return fmt.Errorf("report progress failed with status %s: %s", resp.Status, strings.TrimSpace(string(msg)))
}

// CheckpointSession writes a session's durable position and its new events.
//
// Like ReportResult it is fenced by the lease token, so a daemon that has lost
// the task cannot write over the run that replaced it. A 409 means exactly
// that, and is returned as ErrLeaseLost so the caller can stop rather than
// retry into a wall.
func (c *Client) CheckpointSession(ctx context.Context, req SessionCheckpointReq) error {
	resp, _, err := c.signedPost(ctx, "/api/v1/daemon/session", req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK, http.StatusNoContent:
		return nil
	case http.StatusConflict:
		return ErrLeaseLost
	}
	msg, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
	return fmt.Errorf("session checkpoint failed with status %s: %s", resp.Status, strings.TrimSpace(string(msg)))
}

// LoadSession asks whether a task already has a session to resume. A task on
// its first lease has none, which is reported as Found=false rather than as an
// error — that is the common case, not a fault.
func (c *Client) LoadSession(ctx context.Context, taskID, sessionID, signPubKey string) (*SessionStateRes, error) {
	resp, _, err := c.signedPost(ctx, "/api/v1/daemon/session/load", SessionCheckpointReq{
		SessionID:  sessionID,
		TaskID:     taskID,
		SignPubKey: signPubKey,
	})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNoContent {
		return &SessionStateRes{}, nil
	}
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("session load failed with status %s: %s", resp.Status, strings.TrimSpace(string(msg)))
	}

	var res SessionStateRes
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return nil, fmt.Errorf("decode session state: %w", err)
	}
	return &res, nil
}

// ErrNoInstallation means the Control Plane has no GitHub App installation
// covering this task's repository, so the caller should use its sealed
// GIT_TOKEN instead. It is the ordinary answer for a non-GitHub remote, for an
// org that has not installed the App, and for any deployment where no App is
// configured at all, which is why it is a typed signal rather than an error to
// report.
var ErrNoInstallation = errors.New("no github app installation for this repository")

// ErrInstallationRevoked means the customer removed the App or dropped this
// repository from it. Unlike ErrNoInstallation there is no fallback worth
// trying: an org that connected GitHub and then revoked it almost certainly has
// no PAT, and a git error naming nothing is the worst way to learn that.
var ErrInstallationRevoked = errors.New("github app access was revoked for this repository")

// GitToken exchanges the lease this daemon holds for a short-lived credential
// to the task's repository.
//
// Called immediately before each git operation rather than once per task. An
// installation token lives about an hour and the push happens at the very end
// of a run, so minting once up front trades a cheap call for the worst failure
// shape there is: the work succeeds and the push 401s.
func (c *Client) GitToken(ctx context.Context, req GitTokenReq) (GitTokenResp, error) {
	resp, _, err := c.signedPost(ctx, "/api/v1/daemon/git-token", req)
	if err != nil {
		return GitTokenResp{}, err
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
		var out GitTokenResp
		if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&out); err != nil {
			return GitTokenResp{}, fmt.Errorf("decode git token response: %w", err)
		}
		if out.Token == "" {
			return GitTokenResp{}, errors.New("control plane returned an empty git token")
		}
		return out, nil
	case http.StatusNotFound:
		return GitTokenResp{}, ErrNoInstallation
	case http.StatusGone:
		return GitTokenResp{}, ErrInstallationRevoked
	}

	msg, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
	if resp.StatusCode == http.StatusConflict {
		return GitTokenResp{}, fmt.Errorf("%w: %s", ErrLeaseLost, strings.TrimSpace(string(msg)))
	}
	return GitTokenResp{}, fmt.Errorf("git token request failed with status %s: %s", resp.Status, strings.TrimSpace(string(msg)))
}

// TelemetryDue asks the Control Plane what telemetry polls are due for this
// daemon's org right now. Lease-free like Heartbeat: 204 means nothing is
// due (not an error), 200 decodes the due list, anything else is wrapped.
func (c *Client) TelemetryDue(ctx context.Context, req TelemetryDueReq) (*TelemetryDueRes, error) {
	resp, _, err := c.signedPost(ctx, "/api/v1/daemon/telemetry/due", req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNoContent {
		return &TelemetryDueRes{}, nil
	}
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("telemetry due failed with status %s: %s", resp.Status, strings.TrimSpace(string(msg)))
	}

	var res TelemetryDueRes
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return nil, fmt.Errorf("decode telemetry due response: %w", err)
	}
	return &res, nil
}

// TelemetryReport posts the results of the polls this daemon executed back
// to the Control Plane. Like TelemetryDue it carries no lease/fencing
// branches — telemetry polling is not tied to the task lease queue.
func (c *Client) TelemetryReport(ctx context.Context, req TelemetryReportReq) error {
	resp, _, err := c.signedPost(ctx, "/api/v1/daemon/telemetry/report", req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusNoContent {
		return nil
	}
	msg, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
	return fmt.Errorf("telemetry report failed with status %s: %s", resp.Status, strings.TrimSpace(string(msg)))
}
