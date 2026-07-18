package daemon

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ibreakthecloud/kiwi/pkg/agent"
)

func TestDaemon_LeaseRenewal(t *testing.T) {
	var renewCount int32

	mockSpec := agent.WorkerSpec{
		ID:    "task-renew-test",
		Model: "sonnet",
		// A sleep loop so the task runs long enough to trigger a renew
		Task:    "sleep 2",
		RepoURL: "", // Use fallback sandbox
		Ref:     "",
	}

	// Override smokeCommand to execute sleep directly
	origSmokeCommand := smokeCommand
	smokeCommand = `sleep 2`
	defer func() { smokeCommand = origSmokeCommand }()

	// Disable Docker for the test so sleep runs locally
	os.Setenv("USE_DOCKER", "false")
	defer os.Unsetenv("USE_DOCKER")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/renew") {
			atomic.AddInt32(&renewCount, 1)
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if strings.HasSuffix(r.URL.Path, "/result") {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		// Otherwise, heartbeat
		res := HeartbeatRes{
			Specs:   []agent.WorkerSpec{mockSpec},
			LeaseID: "lease-123",
		}
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(res)
	}))
	defer server.Close()

	cacheDir := t.TempDir()
	cfg := Config{
		APIURL:        server.URL,
		KeyPath:       "", // ephemeral key
		CacheDir:      cacheDir,
		RenewInterval: 500 * time.Millisecond, // very short renew interval for test
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

	success := d.pollCP(ctx)
	if !success {
		t.Fatalf("pollCP returned false")
	}

	// wait a bit for background goroutines to finish up if needed
	time.Sleep(100 * time.Millisecond)

	count := atomic.LoadInt32(&renewCount)
	if count < 2 {
		t.Errorf("expected at least 2 renew calls during the 2 second sleep task, got %d", count)
	}

	// Clean up fallback dir
	fallbackPath := filepath.Join(os.TempDir(), "kiwi-sandbox", mockSpec.ID)
	os.RemoveAll(fallbackPath)
}
