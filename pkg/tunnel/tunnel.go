package tunnel

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Tunnel represents a reverse credential tunnel for a specific task.
type Tunnel struct {
	Requests  chan string
	Responses chan string
	Connected bool
	Mutex     sync.Mutex // Protects Connected

	reqMu   sync.Mutex        // Serializes GetSecret requests to prevent response mismatch
	Cache   map[string]string // Cache resolved secrets to allow disconnected cloud executions
	CacheMu sync.Mutex        // Protects Cache
}

// TunnelRegistry is a thread-safe map of TaskID -> *Tunnel.
type TunnelRegistry struct {
	mu      sync.RWMutex
	tunnels map[string]*Tunnel
}

// NewTunnelRegistry creates a new TunnelRegistry.
func NewTunnelRegistry() *TunnelRegistry {
	return &TunnelRegistry{
		tunnels: make(map[string]*Tunnel),
	}
}

// GlobalRegistry is the global instance of the tunnel registry.
var GlobalRegistry = NewTunnelRegistry()

// Get retrieves a Tunnel for the given taskID, returning nil if it does not exist.
func (r *TunnelRegistry) Get(taskID string) *Tunnel {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.tunnels[taskID]
}

// Register retrieves a Tunnel for the given taskID, creating it if it does not exist.
func (r *TunnelRegistry) Register(taskID string) *Tunnel {
	r.mu.Lock()
	defer r.mu.Unlock()
	if t, exists := r.tunnels[taskID]; exists {
		return t
	}
	t := &Tunnel{
		Requests:  make(chan string),
		Responses: make(chan string),
		Cache:     make(map[string]string),
	}
	r.tunnels[taskID] = t
	return t
}

// GetSecret requests a secret through the tunnel. It blocks until the secret is
// returned, the context is canceled, or a timeout occurs.
func (t *Tunnel) GetSecret(ctx context.Context, key string) (string, error) {
	// 1. Check cache first to avoid tunnel request if secret is already resolved
	t.CacheMu.Lock()
	if val, exists := t.Cache[key]; exists {
		t.CacheMu.Unlock()
		return val, nil
	}
	t.CacheMu.Unlock()

	t.reqMu.Lock()
	defer t.reqMu.Unlock()

	// Double check cache under lock in case it was resolved concurrently
	t.CacheMu.Lock()
	if val, exists := t.Cache[key]; exists {
		t.CacheMu.Unlock()
		return val, nil
	}
	t.CacheMu.Unlock()

	t.Mutex.Lock()
	connected := t.Connected
	t.Mutex.Unlock()

	if !connected {
		return "", fmt.Errorf("tunnel not connected")
	}

	// Send request key
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case t.Requests <- key:
	case <-time.After(5 * time.Second):
		return "", fmt.Errorf("tunnel timeout: request not sent")
	}

	// Receive response value
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case val := <-t.Responses:
		t.CacheMu.Lock()
		t.Cache[key] = val
		t.CacheMu.Unlock()
		return val, nil
	case <-time.After(10 * time.Second):
		return "", fmt.Errorf("tunnel timeout: no response received")
	}
}

// HandleTunnelConn handles 'GET /tunnel/{taskID}'.
// The local CLI client connects and keeps the connection open.
func HandleTunnelConn(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse taskID from URL path "/tunnel/{taskID}"
	path := strings.TrimPrefix(r.URL.Path, "/tunnel/")
	if path == "" || strings.Contains(path, "/") {
		http.Error(w, "Bad request: invalid task ID", http.StatusBadRequest)
		return
	}
	taskID := path

	tunnel := GlobalRegistry.Register(taskID)

	tunnel.Mutex.Lock()
	tunnel.Connected = true
	tunnel.Mutex.Unlock()

	defer func() {
		tunnel.Mutex.Lock()
		tunnel.Connected = false
		tunnel.Mutex.Unlock()
	}()

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/plain")
	w.Header().Set("Transfer-Encoding", "chunked")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	for {
		select {
		case <-r.Context().Done():
			return
		case reqKey, ok := <-tunnel.Requests:
			if !ok {
				return
			}
			_, err := fmt.Fprintln(w, reqKey)
			if err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

// HandleTunnelResponse handles 'POST /tunnel/{taskID}/response'.
// The local CLI client posts the secret response.
func HandleTunnelResponse(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Parse taskID from URL path "/tunnel/{taskID}/response"
	path := strings.TrimPrefix(r.URL.Path, "/tunnel/")
	parts := strings.Split(path, "/")
	if len(parts) != 2 || parts[1] != "response" {
		http.Error(w, "Bad request: invalid response path", http.StatusBadRequest)
		return
	}
	taskID := parts[0]

	tunnel := GlobalRegistry.Get(taskID)
	if tunnel == nil {
		http.Error(w, "Tunnel not found", http.StatusNotFound)
		return
	}

	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read body", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	responseVal := string(bodyBytes)

	select {
	case tunnel.Responses <- responseVal:
		w.WriteHeader(http.StatusOK)
	case <-time.After(5 * time.Second):
		http.Error(w, "Response timeout", http.StatusRequestTimeout)
	}
}

// ConnectAndListen connects to the remote server URL, listens for secret requests,
// looks them up via the getSecret hook, and posts responses back to the server.
func ConnectAndListen(ctx context.Context, serverURL, taskID, authToken string, getSecret func(string) string) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		err := func() error {
			connURL := fmt.Sprintf("%s/tunnel/%s", strings.TrimSuffix(serverURL, "/"), taskID)
			req, err := http.NewRequestWithContext(ctx, "GET", connURL, nil)
			if err != nil {
				return err
			}
			if authToken != "" {
				req.Header.Set("Authorization", "Bearer "+authToken)
			}

			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				return err
			}
			defer resp.Body.Close()

			if resp.StatusCode != http.StatusOK {
				return fmt.Errorf("unexpected status: %s", resp.Status)
			}

			reader := bufio.NewReader(resp.Body)
			for {
				line, err := reader.ReadString('\n')
				if err != nil {
					return err // connection dropped, will trigger reconnect
				}
				reqKey := strings.TrimSpace(line)
				if reqKey == "" {
					continue
				}

				secretVal := getSecret(reqKey)

				postURL := fmt.Sprintf("%s/tunnel/%s/response", strings.TrimSuffix(serverURL, "/"), taskID)
				postReq, err := http.NewRequestWithContext(ctx, "POST", postURL, strings.NewReader(secretVal))
				if err != nil {
					return err
				}
				postReq.Header.Set("Content-Type", "text/plain")
				if authToken != "" {
					postReq.Header.Set("Authorization", "Bearer "+authToken)
				}

				postResp, err := http.DefaultClient.Do(postReq)
				if err != nil {
					return err
				}
				postResp.Body.Close()
			}
		}()

		if err != nil {
			// Back off briefly before attempting reconnection
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(1 * time.Second):
			}
		}
	}
}
