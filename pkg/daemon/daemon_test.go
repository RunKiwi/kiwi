package daemon

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ibreakthecloud/kiwi/pkg/agent"
)

func TestDaemon_pollCP(t *testing.T) {
	// Setup mock control plane
	mockSpec := agent.WorkerSpec{
		ID:      "task-test-poll",
		Model:   "sonnet",
		Task:    "echo 'test'",
		RepoURL: "", // Use fallback to avoid full git clone in fast unit tests
		Ref:     "",
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		res := HeartbeatRes{
			Specs: []agent.WorkerSpec{mockSpec},
		}
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(res)
	}))
	defer server.Close()

	cacheDir := t.TempDir()
	cfg := Config{
		APIURL:   server.URL,
		KeyPath:  "", // ephemeral key
		CacheDir: cacheDir,
	}

	d, err := New(cfg)
	if err != nil {
		t.Fatalf("New daemon failed: %v", err)
	}

	if err := d.Start(); err != nil {
		t.Fatalf("Start daemon failed: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	success := d.pollCP(ctx, HeartbeatReq{PubKey: "test"})
	if !success {
		t.Fatalf("pollCP returned false")
	}

	// Verify fallback sandbox directory was created
	fallbackPath := filepath.Join(os.TempDir(), "kiwi-sandbox", mockSpec.ID)
	if stat, err := os.Stat(fallbackPath); err != nil || !stat.IsDir() {
		t.Errorf("fallback sandbox dir was not created")
	}

	// Clean up fallback dir
	os.RemoveAll(fallbackPath)
}
