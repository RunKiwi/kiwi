package daemon

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ibreakthecloud/kiwi/pkg/agent"
	"github.com/ibreakthecloud/kiwi/pkg/provider"
	"github.com/ibreakthecloud/kiwi/pkg/session"
)

// sessionMock is the daemon-side test double for a model that can both plan and
// implement. One type covers both because executeSession resolves the two
// roles through the same newProvider seam, so in this test they are
// literally the same value — StartConversation (below) tells the roles apart
// by which tool set they were offered.
type sessionMock struct {
	provider.MockToolRunner
	// specs are returned by Complete in order — one per Architect turn, so a
	// test can plan a round and then approve or abandon what came back.
	specs []session.Spec
	calls int
}

func (m *sessionMock) GetCodeEdit(ctx context.Context, task, fileName, code, buildOutput string) (string, error) {
	return "", nil
}

func (m *sessionMock) Complete(ctx context.Context, system, user string) (string, error) {
	spec := m.specs[len(m.specs)-1]
	if m.calls < len(m.specs) {
		spec = m.specs[m.calls]
	}
	m.calls++
	b, err := json.Marshal(spec)
	return string(b), err
}

// StartConversation shadows the embedded MockToolRunner's, so sessionMock can
// serve the Architect's tool-calling path too — the Architect's ArchitectTools
// only ever offers list_files/read_file/grep, which is what distinguishes an
// Architect conversation from the Implementer's larger tool set here. An
// Architect turn answers directly from the scripted specs, the same as
// Complete always did; anything else is the Implementer's Script.
func (m *sessionMock) StartConversation(system string, tools []provider.ToolDef, opts provider.ConversationOpts) provider.ToolConversation {
	if isArchitectToolset(tools) {
		return &architectMockConversation{m: m}
	}
	return m.MockToolRunner.StartConversation(system, tools, opts)
}

func isArchitectToolset(tools []provider.ToolDef) bool {
	if len(tools) == 0 {
		return false
	}
	for _, d := range tools {
		if d.Name != session.ToolListFiles && d.Name != session.ToolReadFile && d.Name != session.ToolGrep {
			return false
		}
	}
	return true
}

// architectMockConversation answers a single turn from sessionMock.Complete,
// with no tool calls of its own — the scripted specs assume no exploration,
// which keeps every existing session test's round count exactly as scripted.
type architectMockConversation struct{ m *sessionMock }

func (c *architectMockConversation) Send(ctx context.Context, text string, results []provider.ToolResult) (provider.Turn, error) {
	resp, err := c.m.Complete(ctx, "", text)
	return provider.Turn{Text: resp}, err
}
func (c *architectMockConversation) Usage() provider.ToolUsage { return provider.ToolUsage{} }
func (c *architectMockConversation) Turns() int                { return 1 }

// newSessionTestDaemon wires a Daemon whose only model is the given double and
// whose test command runs locally rather than in Docker, so executeTask drives
// the real Architect/Implementer loop end to end without a network.
func newSessionTestDaemon(t *testing.T, m *sessionMock) *Daemon {
	t.Helper()
	t.Setenv("USE_DOCKER", "false")
	d, err := New(Config{CacheDir: t.TempDir(), KeyPath: ""})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	d.newProvider = func(creds map[string]string, model, providerID string) (provider.Provider, provider.Critic) {
		return m, nil
	}
	return d
}

// seedWorkspace creates the fallback (no-repo) workspace for a spec id and puts
// one committed file in it.
//
// It is a real git repository because a session is: the Architect reviews the
// accumulated diff, so the runner reads HEAD before the first round and fails
// the task outright if there is nothing to diff against.
func seedWorkspace(t *testing.T, specID, name, content string) string {
	t.Helper()
	workdir := filepath.Join(os.TempDir(), "kiwi-sandbox", specID)
	if err := os.MkdirAll(workdir, 0o755); err != nil {
		t.Fatalf("mkdir workdir: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(workdir) })
	path := filepath.Join(workdir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("seed %s: %v", name, err)
	}
	for _, args := range [][]string{
		{"init", "-q"},
		{"config", "user.email", "test@kiwi.local"},
		{"config", "user.name", "kiwi test"},
		{"add", "."},
		{"commit", "-q", "-m", "seed"},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = workdir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	return path
}

// The end-to-end property that matters: the Implementer's edit lands on disk,
// the test command verifies it, and the task reports success.
func TestExecuteTask_SessionEditsFileUntilTestPasses(t *testing.T) {
	specID := "session-task-1"
	target := seedWorkspace(t, specID, "main.go", "package x // broken\n")

	m := &sessionMock{specs: []session.Spec{
		{Verdict: session.VerdictProceed, Objective: "add the FIXED marker"},
		{Verdict: session.VerdictApprove, Summary: "marker added"},
	}}
	m.Script = func(n int, text string, results []provider.ToolResult) (provider.Turn, error) {
		switch n {
		case 1:
			return provider.Turn{Calls: []provider.ToolCall{
				provider.MockCall("c1", session.ToolWriteFile, map[string]string{
					"path": "main.go", "content": "package x // FIXED\n",
				}),
			}}, nil
		default:
			return provider.Turn{Calls: []provider.ToolCall{
				provider.MockCall("c2", session.ToolFinish, map[string]string{"note": "done"}),
			}}, nil
		}
	}

	d := newSessionTestDaemon(t, m)
	spec := agent.WorkerSpec{
		ID: specID, Model: "sonnet", Task: "add the FIXED marker",
		TestCmd: "grep -q FIXED main.go",
	}
	res := d.executeTask(context.Background(), spec, map[string]string{"ANTHROPIC_API_KEY": "test-key"}, &progressReporter{}, "")

	// res.ok is deliberately NOT the assertion. It now also covers delivery, and
	// this daemon has no remote to open a pull request against — so the run stops
	// at the git token with the whole session behind it already done. Reaching
	// that specific stop is itself the proof: the Implementer's tool call was
	// dispatched, the test command verified the result, and the Architect
	// approved. Anything that failed earlier reports a different detail.
	if !strings.Contains(res.detail, "GIT_TOKEN") && !strings.Contains(res.detail, "git-token") {
		t.Fatalf("session did not reach delivery; detail = %q", res.detail)
	}

	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read target: %v", err)
	}
	if want := "package x // FIXED\n"; string(got) != want {
		t.Errorf("file = %q, want %q", got, want)
	}
}

func TestExecuteTask_FailsWithClearReasonWhenNoProviderKey(t *testing.T) {
	// A model whose provider has no key in the bundle must fail with a precise,
	// actionable reason — not a smoke run that pretends to succeed.
	t.Setenv("USE_DOCKER", "false")
	d, err := New(Config{CacheDir: t.TempDir(), KeyPath: ""}) // real defaultProvider
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	specID := "no-key-task"
	t.Cleanup(func() { os.RemoveAll(filepath.Join(os.TempDir(), "kiwi-sandbox", specID)) })

	spec := agent.WorkerSpec{ID: specID, Model: "sonnet", Task: "x", File: "main.go", TestCmd: "true"}
	res := d.executeTask(context.Background(), spec, map[string]string{}, &progressReporter{}, "") // no ANTHROPIC_API_KEY
	ok, detail := res.ok, res.detail

	if ok {
		t.Fatal("expected failure when the model's provider has no key")
	}
	if !strings.Contains(detail, "no API key") || !strings.Contains(detail, "Anthropic") {
		t.Errorf("detail should name the missing provider key, got %q", detail)
	}
}

func TestExecuteTask_SessionFailsWhenTestNeverPasses(t *testing.T) {
	// The Implementer writes content that never satisfies the check and the
	// Architect keeps asking for another round. The task must report failure
	// with a reason — a FAILED task that explains nothing is the false-green
	// this test exists to prevent.
	specID := "session-task-fail"
	seedWorkspace(t, specID, "main.go", "broken\n")

	m := &sessionMock{specs: []session.Spec{
		{Verdict: session.VerdictProceed, Objective: "add the FIXED marker"},
	}}
	m.Script = func(n int, text string, results []provider.ToolResult) (provider.Turn, error) {
		return provider.Turn{Calls: []provider.ToolCall{
			provider.MockCall("c1", session.ToolWriteFile, map[string]string{
				"path": "main.go", "content": "still broken\n",
			}),
		}}, nil
	}

	d := newSessionTestDaemon(t, m)
	spec := agent.WorkerSpec{
		ID: specID, Model: "sonnet", Task: "impossible",
		TestCmd: "grep -q FIXED main.go",
	}
	res := d.executeTask(context.Background(), spec, map[string]string{"ANTHROPIC_API_KEY": "k"}, &progressReporter{}, "")
	if res.ok {
		t.Fatal("expected failure when the test never passes")
	}
	if res.detail == "" {
		t.Error("expected a non-empty failure detail so the FAILED task explains itself")
	}
}

func TestTruncateDetail(t *testing.T) {
	if got := truncateDetail("short"); got != "short" {
		t.Errorf("short string should pass through, got %q", got)
	}
	long := strings.Repeat("x", maxDetailLen+50)
	got := truncateDetail(long)
	if !strings.HasSuffix(got, "…(truncated)") {
		t.Errorf("over-long string should be marked truncated, got suffix %q", got[len(got)-20:])
	}
	if len([]rune(got)) != maxDetailLen+len([]rune("…(truncated)")) {
		t.Errorf("truncated length = %d runes, want %d", len([]rune(got)), maxDetailLen+len([]rune("…(truncated)")))
	}
}

type multiFileProvider struct{ jsonResponse string }

func (p *multiFileProvider) GetCodeEdit(ctx context.Context, task, fileName, code, buildOutput string) (string, error) {
	return "", nil
}
func (p *multiFileProvider) Complete(ctx context.Context, system, user string) (string, error) {
	return p.jsonResponse, nil
}
