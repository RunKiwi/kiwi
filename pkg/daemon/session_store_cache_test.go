package daemon

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/ibreakthecloud/kiwi/pkg/agent"
	"github.com/ibreakthecloud/kiwi/pkg/provider"
	"github.com/ibreakthecloud/kiwi/pkg/sandbox"
	"github.com/ibreakthecloud/kiwi/pkg/session"
)

func TestReportResult_CacheTokenSplit(t *testing.T) {
	var reportedReq ResultReq
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/daemon/result" {
			if err := json.NewDecoder(r.Body).Decode(&reportedReq); err != nil {
				t.Fatalf("decode ResultReq: %v", err)
			}
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	d, err := New(Config{APIURL: server.URL, CacheDir: t.TempDir()})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if err := d.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	out := taskResult{
		ok:                 true,
		detail:             "success",
		cachedPromptTokens: 450,
		rawPromptTokens:    120,
	}
	d.reportResult(context.Background(), "task-1", "lease-1", out)

	if reportedReq.CachedPromptTokens != 450 {
		t.Fatalf("expected CachedPromptTokens 450, got %d", reportedReq.CachedPromptTokens)
	}
	if reportedReq.RawPromptTokens != 120 {
		t.Fatalf("expected RawPromptTokens 120, got %d", reportedReq.RawPromptTokens)
	}
}

func TestInvestigationOutcomePreservesCacheSplit(t *testing.T) {
	res := session.Result{
		Success:        true,
		NoDiffExpected: true,
		Summary:        "Root cause found in query planner",
		Usage: provider.ToolUsage{
			InputTokens:      100,
			CacheReadTokens:  300,
			CacheWriteTokens: 50,
		},
	}
	out, matched := investigationOutcome(res)
	if !matched {
		t.Fatal("expected match for investigation outcome")
	}
	if out.cachedPromptTokens != 350 {
		t.Fatalf("expected cachedPromptTokens 350, got %d", out.cachedPromptTokens)
	}
	if out.rawPromptTokens != 100 {
		t.Fatalf("expected rawPromptTokens 100, got %d", out.rawPromptTokens)
	}
}

type cacheMockConversation struct {
	turns  int
	usage  provider.ToolUsage
	script func(n int, text string, results []provider.ToolResult) (provider.Turn, error)
}

func (c *cacheMockConversation) TranscriptTokens() int64   { return 0 }
func (c *cacheMockConversation) Turns() int                { return c.turns }
func (c *cacheMockConversation) Usage() provider.ToolUsage { return c.usage }
func (c *cacheMockConversation) Send(ctx context.Context, text string, results []provider.ToolResult) (provider.Turn, error) {
	c.turns++
	if c.script != nil {
		return c.script(c.turns, text, results)
	}
	return provider.Turn{Done: true}, nil
}

type cacheSessionMock struct {
	sessionMock
	usage provider.ToolUsage
}

func (m *cacheSessionMock) StartConversation(system string, tools []provider.ToolDef, opts provider.ConversationOpts) provider.ToolConversation {
	if isArchitectToolset(tools) {
		return &architectMockConversation{m: &m.sessionMock}
	}
	return &cacheMockConversation{
		usage: m.usage,
		script: func(n int, text string, results []provider.ToolResult) (provider.Turn, error) {
			if m.Script != nil {
				return m.Script(n, text, results)
			}
			return provider.Turn{Done: true}, nil
		},
	}
}

func TestExecuteSessionPreservesCacheSplit(t *testing.T) {
	specID := "session-task-cache-split"
	target := seedWorkspace(t, specID, "main.go", "package x // broken\n")

	m := &cacheSessionMock{
		sessionMock: sessionMock{
			specs: []session.Spec{
				{Verdict: session.VerdictProceed, Objective: "fix the bug"},
				{Verdict: session.VerdictApprove, Summary: "bug fixed"},
			},
		},
		usage: provider.ToolUsage{
			InputTokens:      150,
			CacheReadTokens:  400,
			CacheWriteTokens: 60,
		},
	}
	m.Script = func(n int, text string, results []provider.ToolResult) (provider.Turn, error) {
		switch n {
		case 1:
			return provider.Turn{Calls: []provider.ToolCall{
				provider.MockCall("c1", session.ToolWriteFile, map[string]string{
					"path": "main.go", "content": "package x // fixed\n",
				}),
			}}, nil
		default:
			return provider.Turn{Calls: []provider.ToolCall{
				provider.MockCall("c2", session.ToolFinish, map[string]string{"note": "done"}),
			}}, nil
		}
	}

	d := newSessionTestDaemon(t, &m.sessionMock)
	d.newProvider = func(creds map[string]string, model, providerID string) (provider.Provider, provider.Critic) {
		return m, nil
	}

	spec := agent.WorkerSpec{
		ID:      specID,
		Model:   "sonnet",
		Task:    "fix the bug",
		TestCmd: "grep -q fixed main.go",
	}

	prog := &progressReporter{}
	deps := sessionDeps{
		leaseID:      "lease-1",
		worktreePath: filepath.Dir(target),
		sandboxCfg:   &sandbox.SandboxConfig{UseDocker: false},
		testCmd:      "grep -q fixed main.go",
		verify:       func(ctx context.Context) (string, bool, error) { return "", true, nil },
	}

	result := d.executeSession(context.Background(), spec, map[string]string{"ANTHROPIC_API_KEY": "test-key"}, prog, deps)

	if result.cachedPromptTokens != 460 {
		t.Fatalf("expected cachedPromptTokens 460 (400+60), got %d", result.cachedPromptTokens)
	}
	if result.rawPromptTokens != 150 {
		t.Fatalf("expected rawPromptTokens 150, got %d", result.rawPromptTokens)
	}
}
