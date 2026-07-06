package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
)

// Client talks to the kiwid daemon HTTP API.
type Client struct {
	ServerURL      string
	Token          string
	IdempotencyKey string
	HTTP           *http.Client
}

// TaskStatus is the subset of the daemon's task row the client consumes.
type TaskStatus struct {
	ID     string  `json:"id"`
	Status string  `json:"status"`
	Logs   string  `json:"logs"`
	Cost   float64 `json:"cost"`
}

// New builds a Client with a default HTTP client.
func New(serverURL, token string) *Client {
	return &Client{
		ServerURL: strings.TrimSuffix(serverURL, "/"),
		Token:     token,
		HTTP:      http.DefaultClient,
	}
}

func (c *Client) authErr(resp *http.Response) error {
	body, _ := io.ReadAll(resp.Body)
	msg := strings.TrimSpace(string(body))
	if resp.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("daemon returned 401 Unauthorized (check -token): %s", msg)
	}
	return fmt.Errorf("daemon returned %d: %s", resp.StatusCode, msg)
}

// SubmitTask uploads the zipped codebase and task parameters, returning the task ID.
func (c *Client) SubmitTask(ctx context.Context, task, file, testCmd string, codebase []byte) (string, error) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	for k, v := range map[string]string{"task": task, "file": file, "test_cmd": testCmd} {
		if err := mw.WriteField(k, v); err != nil {
			return "", err
		}
	}
	fw, err := mw.CreateFormFile("codebase", "codebase.zip")
	if err != nil {
		return "", err
	}
	if _, err := fw.Write(codebase); err != nil {
		return "", err
	}
	if err := mw.Close(); err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.ServerURL+"/tasks", &buf)
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+c.Token)
	if c.IdempotencyKey != "" {
		req.Header.Set("Idempotency-Key", c.IdempotencyKey)
	}

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", c.authErr(resp)
	}
	var out struct {
		TaskID string `json:"task_id"`
		Status string `json:"status"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	return out.TaskID, nil
}

// GetStatus fetches the current task row.
func (c *Client) GetStatus(ctx context.Context, taskID string) (TaskStatus, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.ServerURL+"/tasks/"+taskID, nil)
	if err != nil {
		return TaskStatus{}, err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return TaskStatus{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return TaskStatus{}, c.authErr(resp)
	}
	var st TaskStatus
	if err := json.NewDecoder(resp.Body).Decode(&st); err != nil {
		return TaskStatus{}, err
	}
	return st, nil
}

// DownloadResult fetches the fixed codebase zip for a successful task.
func (c *Client) DownloadResult(ctx context.Context, taskID string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.ServerURL+"/tasks/"+taskID+"?download=true", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, c.authErr(resp)
	}
	return io.ReadAll(resp.Body)
}
