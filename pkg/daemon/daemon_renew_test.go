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
	"github.com/ibreakthecloud/kiwi/pkg/provider"
)

// slowActor is a provider.Provider whose GetCodeEdit blocks briefly, standing in
// for a slow model call so executeTask runs long enough to trigger lease
// renewals. It always returns the same content, so the test never passes and the
// loop runs its full step budget — long enough for several renew ticks.
type slowActor struct{ delay time.Duration }

func (s slowActor) GetCodeEdit(ctx context.Context, task, fileName, codeContent, buildOutput string) (string, error) {
	select {
	case <-time.After(s.delay):
	case <-ctx.Done():
		return "", ctx.Err()
	}
	return codeContent, nil
}

func TestDaemon_LeaseRenewal(t *testing.T) {
	var renewCount int32

	mockSpec := agent.WorkerSpec{
		ID:      "task-renew-test",
		Model:   "sonnet",
		Task:    "make the test pass",
		File:    "target.txt",
		TestCmd: "exit 1", // never passes, so the loop runs its full budget
		RepoURL: "",       // fallback sandbox dir
		Ref:     "",
	}

	// Pre-create the fallback worktree + target file the loop reads each step.
	fallbackPath := filepath.Join(os.TempDir(), "kiwi-sandbox", mockSpec.ID)
	if err := os.MkdirAll(fallbackPath, 0o755); err != nil {
		t.Fatalf("mkdir fallback: %v", err)
	}
	if err := os.WriteFile(filepath.Join(fallbackPath, "target.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write target: %v", err)
	}
	defer os.RemoveAll(fallbackPath)

	// Run the test command locally (no Docker) so "exit 1" is cheap.
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
		// Otherwise, heartbeat.
		res := HeartbeatRes{Specs: []agent.WorkerSpec{mockSpec}, LeaseID: "lease-123"}
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(res)
	}))
	defer server.Close()

	cfg := Config{
		APIURL:        server.URL,
		KeyPath:       "", // ephemeral key
		CacheDir:      t.TempDir(),
		MaxSteps:      3,
		RenewInterval: 300 * time.Millisecond, // short so several ticks fit the run
	}

	d, err := New(cfg)
	if err != nil {
		t.Fatalf("New daemon failed: %v", err)
	}
	// Inject a slow actor so the real loop drives executeTask for ~1.5s.
	d.newProvider = func(map[string]string, string) (provider.Provider, provider.Critic) {
		return slowActor{delay: 500 * time.Millisecond}, nil
	}
	if err := d.Start(); err != nil {
		t.Fatalf("Start daemon failed: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if !d.pollCP(ctx) {
		t.Fatalf("pollCP returned false")
	}
	time.Sleep(100 * time.Millisecond) // let the last renew goroutine settle

	if count := atomic.LoadInt32(&renewCount); count < 2 {
		t.Errorf("expected at least 2 renew calls during the multi-step loop, got %d", count)
	}
}
