package daemon

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ibreakthecloud/kiwi/pkg/agent"
	"github.com/ibreakthecloud/kiwi/pkg/provider"
)

type mockProviderJSON struct {
	response string
}

func (m *mockProviderJSON) GetCodeEdit(ctx context.Context, task string, fileName string, codeContent string, buildOutput string) (string, error) {
	return "", nil
}

func (m *mockProviderJSON) Complete(ctx context.Context, system, user string) (string, error) {
	return m.response, nil
}

func TestDiscoverTargetFiles(t *testing.T) {
	tree := []string{"main.go", "utils.go", "README.md"}

	tests := []struct {
		name     string
		response string
		expected []string
	}{
		{
			name:     "valid json",
			response: `["main.go", "utils.go"]`,
			expected: []string{"main.go", "utils.go"},
		},
		{
			name:     "with prose",
			response: "Here is the result:\n```json\n[\"main.go\"]\n```",
			expected: []string{"main.go"},
		},
		{
			name:     "filters out of tree and escapes",
			response: `["main.go", "not_in_tree.go", "../escape.go"]`,
			expected: []string{"main.go"},
		},
		{
			name:     "caps at 6",
			response: `["main.go", "utils.go", "README.md", "a", "b", "c", "d"]`,
			expected: []string{"main.go", "utils.go", "README.md"}, // Only 3 are in tree
		},
		{
			name:     "invalid json",
			response: `this is not json`,
			expected: nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			actor := &mockProviderJSON{response: tc.response}
			res, err := discoverTargetFiles(context.Background(), actor, "fix", tree)
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if len(res) != len(tc.expected) {
				t.Errorf("len(res)=%d, want %d", len(res), len(tc.expected))
			}
			for i := range res {
				if res[i] != tc.expected[i] {
					t.Errorf("res[%d]=%q, want %q", i, res[i], tc.expected[i])
				}
			}
		})
	}
}

func TestExecuteTaskEmptyDiscoveryHonestFailure(t *testing.T) {
	d := newExecTestDaemon(t, "")
	d.newProvider = func(creds map[string]string, model string) (provider.Provider, provider.Critic) {
		return &mockProviderJSON{response: `[]`}, nil
	}
	specID := "no-file-task-discovery"
	t.Cleanup(func() { os.RemoveAll(filepath.Join(os.TempDir(), "kiwi-sandbox", specID)) })

	// spec.File is empty, spec.Files is nil
	spec := agent.WorkerSpec{ID: specID, Model: "sonnet", Task: "fix it", File: "", TestCmd: "true"}
	ok, _, detail, _ := d.executeTask(context.Background(), spec, map[string]string{"ANTHROPIC_API_KEY": "k"})

	if ok {
		t.Fatal("expected failure when there is no target file")
	}
	if !strings.Contains(detail, "could not identify a file to change from the task description — set one under Advanced options") {
		t.Errorf("detail should explain the missing target file from discovery, got %q", detail)
	}
}
