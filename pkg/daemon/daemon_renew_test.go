package daemon

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ibreakthecloud/kiwi/pkg/agent"
	"github.com/ibreakthecloud/kiwi/pkg/provider"
	"github.com/ibreakthecloud/kiwi/pkg/session"
)

// newSlowSessionMock returns a model double whose every turn blocks briefly,
// standing in for slow model calls so executeTask runs long enough to trigger
// lease renewals. The Architect never approves and the Implementer never
// finishes, so the session runs its full round budget — long enough for several
// renew ticks.
func newSlowSessionMock(delay time.Duration) *sessionMock {
	m := &sessionMock{specs: []session.Spec{
		{Verdict: session.VerdictProceed, Objective: "keep going"},
	}}
	m.Script = func(n int, text string, results []provider.ToolResult) (provider.Turn, error) {
		select {
		case <-time.After(delay):
		}
		return provider.Turn{Calls: []provider.ToolCall{
			provider.MockCall("c1", session.ToolWriteFile, map[string]string{
				"path": "target.txt", "content": "x",
			}),
		}}, nil
	}
	return m
}

func TestDaemon_LeaseRenewal(t *testing.T) {
	var renewCount int32

	mockSpec := agent.WorkerSpec{
		ID:      "task-renew-test",
		Model:   "sonnet",
		Task:    "make the test pass",
		TestCmd: "exit 1", // never passes, so the loop runs its full budget
		RepoURL: "",       // fallback sandbox dir
		Ref:     "",
	}

	// Pre-create the fallback worktree the session works in. It is a real git
	// repository because a session diffs against HEAD before its first round.
	seedWorkspace(t, mockSpec.ID, "target.txt", "x")

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
		MaxRounds:     3,
		RenewInterval: 300 * time.Millisecond, // short so several ticks fit the run
	}

	d, err := New(cfg)
	if err != nil {
		t.Fatalf("New daemon failed: %v", err)
	}
	// Inject a slow model so the real session drives executeTask for ~1.5s.
	slow := newSlowSessionMock(500 * time.Millisecond)
	d.newProvider = func(map[string]string, string, string) (provider.Provider, provider.Critic) {
		return slow, nil
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
