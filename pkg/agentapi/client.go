package agentapi

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

// Client is the sandbox-side client for the Agent API. It carries a single
// per-job scoped token and can only act on its own job.
type Client struct {
	BaseURL string // e.g. http://control-plane:8080
	Token   string // scoped job token (plaintext)
	JobID   string
	HTTP    *http.Client
}

func NewClient(baseURL, token, jobID string) *Client {
	return &Client{
		BaseURL: strings.TrimRight(baseURL, "/"),
		Token:   token,
		JobID:   jobID,
		HTTP:    &http.Client{Timeout: 60 * time.Second},
	}
}

// AppendEvent records an execution-trace event; returns the assigned seq.
func (c *Client) AppendEvent(ctx context.Context, phase string, payload map[string]interface{}) (int64, error) {
	var out struct {
		Seq int64 `json:"seq"`
	}
	err := c.post(ctx, "events", appendEventReq{Phase: phase, Payload: payload}, &out)
	return out.Seq, err
}

// Checkpoint records checkpoint metadata anchored at eventSeq.
func (c *Client) Checkpoint(ctx context.Context, eventSeq int64, state map[string]interface{}, snapshotURI, snapshotHash string) error {
	return c.post(ctx, "checkpoints", checkpointReq{
		EventSeq: eventSeq, State: state, SnapshotURI: snapshotURI, SnapshotHash: snapshotHash,
	}, nil)
}

// FetchSecret retrieves a just-in-time secret for the job.
func (c *Client) FetchSecret(ctx context.Context, key string) (string, error) {
	var out struct {
		Value string `json:"value"`
	}
	err := c.post(ctx, "secrets", fetchSecretReq{Key: key}, &out)
	return out.Value, err
}

// ReportResult posts the terminal status of the job.
func (c *Client) ReportResult(ctx context.Context, status, errMsg string) error {
	return c.post(ctx, "result", reportResultReq{Status: status, Error: errMsg}, nil)
}

func (c *Client) post(ctx context.Context, action string, body, out interface{}) error {
	buf, err := json.Marshal(body)
	if err != nil {
		return err
	}
	url := fmt.Sprintf("%s/agent/%s/%s", c.BaseURL, c.JobID, action)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(buf))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("agent api %s: %s: %s", action, resp.Status, bytes.TrimSpace(msg))
	}
	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}
