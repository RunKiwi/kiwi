package tunnel

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ibreakthecloud/kiwi/pkg/auth"
)

func TestTunnelRoundtrip(t *testing.T) {
	taskID := "test-task-123"

	// Mock claims injection middleware
	mockAuth := func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			claims := &auth.UserClaims{
				UserID: "test-user",
				Email:  "test@example.com",
				Name:   "Test User",
				OrgID:  "test-org",
				Role:   "member",
			}
			r = r.WithContext(auth.ContextWithClaims(r.Context(), claims))
			next(w, r)
		}
	}

	// Create a multiplexer to route requests to handlers
	mux := http.NewServeMux()
	mux.HandleFunc("/tunnel/", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/response") {
			mockAuth(HandleTunnelResponse)(w, r)
		} else {
			mockAuth(HandleTunnelConn)(w, r)
		}
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	// Pre-register tunnel in global registry with correct tenant ID matching mock claims
	GlobalRegistry.Register(taskID, "test-user", "test-org")

	// Define our mock getSecret hook
	secrets := map[string]string{
		"DB_PASSWORD":  "supersecret123",
		"API_KEY":      "key456",
		"EMPTY_SECRET": "",
	}
	getSecretHook := func(key string) string {
		return secrets[key]
	}

	// Start the client in the background
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errChan := make(chan error, 1)
	go func() {
		errChan <- ConnectAndListen(ctx, server.URL, taskID, "dummy-token", getSecretHook)
	}()

	// Wait for the client to connect and mark tunnel as Connected
	var tunnel *Tunnel
	connected := false
	for i := 0; i < 30; i++ {
		tunnel = GlobalRegistry.Get(taskID)
		if tunnel != nil {
			tunnel.Mutex.Lock()
			connected = tunnel.Connected
			tunnel.Mutex.Unlock()
			if connected {
				break
			}
		}
		time.Sleep(100 * time.Millisecond)
	}

	if !connected {
		t.Fatalf("client failed to connect and establish tunnel")
	}

	// Test case 1: Retrieve DB_PASSWORD
	pwd, err := tunnel.GetSecret(ctx, "DB_PASSWORD")
	if err != nil {
		t.Fatalf("failed to get DB_PASSWORD: %v", err)
	}
	if pwd != "supersecret123" {
		t.Errorf("expected DB_PASSWORD to be 'supersecret123', got: %q", pwd)
	}

	// Test case 2: Retrieve API_KEY
	apiKey, err := tunnel.GetSecret(ctx, "API_KEY")
	if err != nil {
		t.Fatalf("failed to get API_KEY: %v", err)
	}
	if apiKey != "key456" {
		t.Errorf("expected API_KEY to be 'key456', got: %q", apiKey)
	}

	// Test case 3: Retrieve empty secret
	emptySec, err := tunnel.GetSecret(ctx, "EMPTY_SECRET")
	if err != nil {
		t.Fatalf("failed to get EMPTY_SECRET: %v", err)
	}
	if emptySec != "" {
		t.Errorf("expected EMPTY_SECRET to be empty, got: %q", emptySec)
	}

	// Test case 4: Request non-existent secret
	missingSec, err := tunnel.GetSecret(ctx, "NON_EXISTENT")
	if err != nil {
		t.Fatalf("failed to get NON_EXISTENT: %v", err)
	}
	if missingSec != "" {
		t.Errorf("expected NON_EXISTENT to be empty, got: %q", missingSec)
	}

	// Shutdown client and verify cleanup
	cancel()
	err = <-errChan
	if err != nil && err != context.Canceled {
		t.Errorf("client exited with unexpected error: %v", err)
	}

	// Verify tunnel is no longer connected (polling to avoid race condition)
	disconnected := false
	for i := 0; i < 30; i++ {
		tunnel.Mutex.Lock()
		connected = tunnel.Connected
		tunnel.Mutex.Unlock()
		if !connected {
			disconnected = true
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	if !disconnected {
		t.Errorf("expected tunnel to be disconnected after client shut down")
	}
}

func TestTunnelNotConnectedError(t *testing.T) {
	taskID := "disconnected-task"
	tunnel := GlobalRegistry.Register(taskID, "user1", "org1")

	// Tunnel is not connected initially
	_, err := tunnel.GetSecret(context.Background(), "SOME_KEY")
	if err == nil {
		t.Errorf("expected GetSecret to fail when tunnel is not connected")
	} else if !strings.Contains(err.Error(), "tunnel not connected") {
		t.Errorf("expected 'tunnel not connected' error, got: %v", err)
	}
}
